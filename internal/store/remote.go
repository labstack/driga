package store

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/model"
)

// PurgeRemote deletes every cached row for a peer, including its cursor.
func (tx *Tx) PurgeRemote(machine string) error {
	if err := tx.ClearRemoteRows(machine); err != nil {
		return err
	}
	return tx.exec("DELETE FROM machines WHERE machine_id=?", machine)
}

// ClearRemoteRows replaces a peer's cache without discarding connection metadata.
func (tx *Tx) ClearRemoteRows(machine string) error {
	if machine == "" || machine == tx.localID {
		return fmt.Errorf("refuse to purge local or empty machine_id")
	}
	for _, table := range []string{"session_files", "handoffs", "inbox_items", "procs", "events", "sessions", "worktrees", "repos", "orphans", "machine_samples", "offsets", "sources", "gaps"} {
		if err := tx.exec("DELETE FROM "+table+" WHERE machine_id=?", machine); err != nil {
			return err
		}
	}
	return nil
}

// PruneRemote applies local history limits even when peers stop publishing.
func (tx *Tx) PruneRemote(now time.Time) error {
	machines, err := tx.Machines()
	if err != nil {
		return err
	}
	for _, m := range machines {
		if m.MachineID == tx.localID {
			continue
		}
		if m.LastContactAt.Before(now.Add(-30 * 24 * time.Hour)) {
			if err := tx.ClearRemoteRows(m.MachineID); err != nil {
				return err
			}
			// A cache cleared by viewer retention must take a fresh snapshot,
			// even if the peer's feed still accepts the old cursor.
			m.FeedEpoch, m.FeedSeq = "", 0
			if err := tx.ForMachine(m.MachineID).UpsertMachine(m); err != nil {
				return err
			}
			continue
		}
		r := tx.ForMachine(m.MachineID)
		if err := r.Prune(now); err != nil {
			return err
		}
		if err := r.exec("DELETE FROM session_files WHERE machine_id=? AND last_edit_at<?", m.MachineID, unix(now.Add(-30*time.Minute))); err != nil {
			return err
		}
		if err := r.exec("DELETE FROM handoffs WHERE machine_id=? AND ts<?", m.MachineID, unix(now.Add(-30*24*time.Hour))); err != nil {
			return err
		}
	}
	return nil
}

// SessionByKey resolves a session within this writer's machine only.
func (tx *Tx) SessionByKey(adapter, external string) (model.Session, error) {
	return scanSession(tx.conn.QueryRowContext(tx.ctx, "SELECT id,"+sessionColumns+" FROM sessions WHERE machine_id=? AND adapter=? AND external_id=?", tx.machineID, adapter, external))
}

// ApplyInbox replaces authoritative peer fields while retaining a pending ack
// until the peer confirms it. Local inbox insertion remains insert-only.
func (tx *Tx) ApplyInbox(i model.InboxItem) error {
	return tx.exec(`INSERT INTO inbox_items(machine_id,id,session_id,kind,ts,summary,acked_at,ack_pending_at) VALUES(?,?,?,?,?,?,?,NULL)
 ON CONFLICT(machine_id,id) DO UPDATE SET session_id=excluded.session_id,kind=excluded.kind,ts=excluded.ts,summary=excluded.summary,
 acked_at=excluded.acked_at,ack_pending_at=CASE WHEN excluded.acked_at IS NULL THEN inbox_items.ack_pending_at ELSE NULL END`, tx.machineID, i.ID, i.SessionID, i.Kind, unix(i.TS), i.Summary, nullableTime(i.AckedAt))
}

// ApplyEvent idempotently stores a natural-keyed peer event.
func (tx *Tx) ApplyEvent(e model.Event) error {
	if err := tx.exec("DELETE FROM events WHERE machine_id=? AND adapter=? AND external_id=? AND ts=? AND kind=?", tx.machineID, e.Adapter, e.ExternalID, unix(e.TS), e.Kind); err != nil {
		return err
	}
	return tx.InsertEvent(e)
}

// ApplyHandoff idempotently stores a peer completion.
func (tx *Tx) ApplyHandoff(id int64, at time.Time, text string) error {
	if err := tx.exec("DELETE FROM handoffs WHERE machine_id=? AND session_id=? AND ts=?", tx.machineID, id, unix(at)); err != nil {
		return err
	}
	return tx.InsertHandoff(id, at, text)
}

// ApplySource replaces all freshness fields, including a cleared peer error.
func (tx *Tx) ApplySource(s model.SourceStatus) error {
	return tx.exec(`INSERT INTO sources(machine_id,name,last_ok_at,last_error,last_error_at) VALUES(?,?,?,?,?)
 ON CONFLICT(machine_id,name) DO UPDATE SET last_ok_at=excluded.last_ok_at,last_error=excluded.last_error,last_error_at=excluded.last_error_at`, tx.machineID, s.Name, unix(s.LastOKAt), s.LastError, unix(s.LastErrorAt))
}

// DeleteRemote removes one natural key within a remote writer. Timestamp keys
// have already been translated into the viewer's clock by the fleet decoder.
func (tx *Tx) DeleteRemote(kind feed.Kind, key string) error {
	if tx.machineID == tx.localID {
		return fmt.Errorf("remote deletion requires a peer writer")
	}
	parts := func(n int) ([]string, error) {
		var v []string
		err := json.Unmarshal([]byte(key), &v)
		if err == nil && len(v) != n {
			err = fmt.Errorf("invalid %s key", kind)
		}
		return v, err
	}
	stamp := func(s string) (int64, error) { at, e := time.Parse(time.RFC3339Nano, s); return unix(at), e }
	switch kind {
	case feed.Session, feed.SessionFiles:
		v, e := parts(2)
		if e != nil {
			return e
		}
		s, e := tx.SessionByKey(v[0], v[1])
		if e == ErrNotFound {
			return nil
		}
		if e != nil {
			return e
		}
		if kind == feed.SessionFiles {
			return tx.exec("DELETE FROM session_files WHERE machine_id=? AND session_id=?", tx.machineID, s.ID)
		}
		if e = tx.exec("DELETE FROM inbox_items WHERE machine_id=? AND session_id=?", tx.machineID, s.ID); e != nil {
			return e
		}
		if e = tx.exec("UPDATE procs SET session_id=0 WHERE machine_id=? AND session_id=?", tx.machineID, s.ID); e != nil {
			return e
		}
		return tx.exec("DELETE FROM sessions WHERE machine_id=? AND id=?", tx.machineID, s.ID)
	case feed.Event:
		v, e := parts(4)
		if e != nil {
			return e
		}
		at, e := stamp(v[2])
		if e != nil {
			return e
		}
		return tx.exec("DELETE FROM events WHERE machine_id=? AND adapter=? AND external_id=? AND ts=? AND kind=?", tx.machineID, v[0], v[1], at, v[3])
	case feed.Handoff:
		v, e := parts(3)
		if e != nil {
			return e
		}
		at, e := stamp(v[2])
		if e != nil {
			return e
		}
		s, e := tx.SessionByKey(v[0], v[1])
		if e == ErrNotFound {
			return nil
		}
		if e != nil {
			return e
		}
		return tx.exec("DELETE FROM handoffs WHERE machine_id=? AND session_id=? AND ts=?", tx.machineID, s.ID, at)
	case feed.Repo:
		if e := tx.exec("UPDATE sessions SET repo_id=NULL,worktree_id=NULL WHERE machine_id=? AND repo_id IN(SELECT id FROM repos WHERE machine_id=? AND path=?)", tx.machineID, tx.machineID, key); e != nil {
			return e
		}
		if e := tx.exec("DELETE FROM worktrees WHERE machine_id=? AND repo_id IN(SELECT id FROM repos WHERE machine_id=? AND path=?)", tx.machineID, tx.machineID, key); e != nil {
			return e
		}
		return tx.exec("DELETE FROM repos WHERE machine_id=? AND path=?", tx.machineID, key)
	case feed.Worktree:
		if e := tx.exec("UPDATE sessions SET worktree_id=NULL WHERE machine_id=? AND worktree_id IN(SELECT id FROM worktrees WHERE machine_id=? AND path=?)", tx.machineID, tx.machineID, key); e != nil {
			return e
		}
		return tx.exec("DELETE FROM worktrees WHERE machine_id=? AND path=?", tx.machineID, key)
	case feed.Inbox, feed.Orphan, feed.Source:
		table, column := "inbox_items", "id"
		if kind == feed.Orphan {
			table = "orphans"
		}
		if kind == feed.Source {
			table, column = "sources", "name"
		}
		return tx.exec("DELETE FROM "+table+" WHERE machine_id=? AND "+column+"=?", tx.machineID, key)
	case feed.Procs:
		return tx.ReplaceProcs(nil)
	case feed.MachineSample:
		at, e := stamp(key)
		if e != nil {
			return e
		}
		return tx.exec("DELETE FROM machine_samples WHERE machine_id=? AND ts=?", tx.machineID, at)
	case feed.Gap:
		v, e := parts(2)
		if e != nil {
			return e
		}
		from, e := stamp(v[0])
		if e != nil {
			return e
		}
		to, e := stamp(v[1])
		if e != nil {
			return e
		}
		return tx.exec("DELETE FROM gaps WHERE machine_id=? AND from_ts=? AND to_ts=?", tx.machineID, from, to)
	default:
		return fmt.Errorf("unsupported delete kind %q", kind)
	}
}

// ExtendRemoteGap coalesces a viewer-observed outage as its end advances.
func (tx *Tx) ExtendRemoteGap(from, to time.Time) error {
	if tx.machineID == tx.localID {
		return fmt.Errorf("remote gap requires a peer writer")
	}
	if to.Before(from) {
		to = from
	}
	if err := tx.exec("DELETE FROM gaps WHERE machine_id=? AND from_ts=? AND to_ts<=?", tx.machineID, unix(from), unix(to)); err != nil {
		return err
	}
	return tx.InsertGap(model.Gap{From: from, To: to})
}
