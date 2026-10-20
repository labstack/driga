package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/model"
)

// SessionFilter selects live sessions, optionally including ended sessions.
// EndedSince also includes sessions ending at or after that time. RepoPath is
// matched against the repository's canonical path, not its worktree paths.
type SessionFilter struct {
	RepoPath     string
	IncludeEnded bool
	EndedSince   time.Time
}

type scanner interface{ Scan(...any) error }

func readMany[T any](q *querier, query string, args []any, scan func(scanner) (T, error)) ([]T, error) {
	rows, err := q.conn.QueryContext(q.ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]T, 0)
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

// upsertID uses only package-owned SQL identifiers. IDs are allocated by SQLite
// and preserved when an existing natural key is updated.
func (tx *Tx) upsertID(table, columns, conflict string, values ...any) (int64, error) {
	cols := strings.Split(columns, ",")
	updates := make([]string, len(cols))
	for i, col := range cols {
		updates[i] = col + "=excluded." + col
	}
	query := "INSERT INTO " + table + " (" + columns + ") VALUES (" + strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",") + ") ON CONFLICT (" + conflict + ") DO UPDATE SET " + strings.Join(updates, ",") + " RETURNING id"
	var id int64
	err := tx.conn.QueryRowContext(tx.ctx, query, values...).Scan(&id)
	return id, err
}

// Scope selects machine IDs; nil selects all and an empty slice selects none.
type Scope struct{ Machines []string }

func (scope Scope) where(column string) (string, []any) {
	if scope.Machines == nil {
		return "1=1", nil
	}
	if len(scope.Machines) == 0 {
		return "1=0", nil
	}
	args := make([]any, len(scope.Machines))
	for i, id := range scope.Machines {
		args[i] = id
	}
	return column + " IN (" + strings.TrimSuffix(strings.Repeat("?,", len(args)), ",") + ")", args
}

func (q *querier) localScope() Scope { return Scope{Machines: []string{q.machineID}} }

const machineColumns = "id,machine_id,hostname,os,arch,cores,mem_total,first_seen,last_seen,name,device_id,addr,driga_version,feed_epoch,feed_seq,last_contact_at,last_error,clock_skew_ms,state"

// UpsertMachine stores host information under the writer's machine ID.
func (tx *Tx) UpsertMachine(m model.Machine) error {
	if m.Name == "" {
		m.Name = m.Hostname
	}
	if m.State == "" {
		m.State = "local"
		if tx.machineID != tx.localID {
			m.State = "stale"
		}
	}
	return tx.exec(`INSERT INTO machines (`+machineColumns+`)
	VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET hostname=excluded.hostname,
	os=excluded.os,arch=excluded.arch,cores=excluded.cores,mem_total=excluded.mem_total,
	first_seen=excluded.first_seen,last_seen=excluded.last_seen,name=excluded.name,device_id=excluded.device_id,
	addr=excluded.addr,driga_version=excluded.driga_version,feed_epoch=excluded.feed_epoch,feed_seq=excluded.feed_seq,
	last_contact_at=excluded.last_contact_at,last_error=excluded.last_error,clock_skew_ms=excluded.clock_skew_ms,state=excluded.state`,
		tx.machineID, tx.machineID, m.Hostname, m.OS, m.Arch, m.Cores, m.MemTotal, unix(m.FirstSeen), unix(m.LastSeen),
		m.Name, m.DeviceID, m.Addr, m.DrigaVersion, m.FeedEpoch, m.FeedSeq, unix(m.LastContactAt), m.LastError, m.ClockSkewMS, m.State)
}

func scanMachine(s scanner) (model.Machine, error) {
	var m model.Machine
	var first, last, contact int64
	err := s.Scan(&m.ID, &m.MachineID, &m.Hostname, &m.OS, &m.Arch, &m.Cores, &m.MemTotal, &first, &last,
		&m.Name, &m.DeviceID, &m.Addr, &m.DrigaVersion, &m.FeedEpoch, &m.FeedSeq, &contact, &m.LastError, &m.ClockSkewMS, &m.State)
	m.FirstSeen, m.LastSeen, m.LastContactAt = fromUnix(first), fromUnix(last), fromUnix(contact)
	return m, err
}

// Machine returns the local host information, or ErrNotFound.
func (q *querier) Machine() (model.Machine, error) {
	return scanMachine(q.conn.QueryRowContext(q.ctx, "SELECT "+machineColumns+" FROM machines WHERE machine_id=?", q.machineID))
}

// Machines returns all known machines in identity order.
func (q *querier) Machines() ([]model.Machine, error) {
	return readMany(q, "SELECT "+machineColumns+" FROM machines ORDER BY machine_id", nil, scanMachine)
}

// ReplaceProcs replaces this machine's entire latest process snapshot.
func (tx *Tx) ReplaceProcs(procs []model.Proc) error {
	if err := tx.exec("DELETE FROM procs WHERE machine_id=?", tx.machineID); err != nil {
		return err
	}
	for _, p := range procs {
		if err := tx.exec(`INSERT INTO procs (machine_id,pid,ppid,start_time,rss,cpu_seconds,cpu_percent,cmdline,exe,role,agent,session_id,cwd,tty)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, tx.machineID, p.PID, p.PPID, unix(p.StartTime), p.RSS, p.CPUSeconds, p.CPUPercent, p.Cmdline, p.Exe, p.Role, p.Agent, p.SessionID, p.Cwd, p.TTY); err != nil {
			return err
		}
	}
	return nil
}

// SyncProcs makes the table hold procs, given that it currently holds prev,
// writing only rows whose stored columns differ. With prev nil it replaces
// every row. Callers must pass the table as last committed, never a guess.
func (tx *Tx) SyncProcs(prev, procs []model.Proc) error {
	if prev == nil {
		return tx.ReplaceProcs(procs)
	}
	old := make(map[int32]model.Proc, len(prev))
	for _, p := range prev {
		old[p.PID] = p
	}
	for _, p := range procs {
		if o, ok := old[p.PID]; ok && sameStoredProc(o, p) {
			delete(old, p.PID)
			continue
		}
		delete(old, p.PID)
		if err := tx.exec(`INSERT INTO procs (machine_id,pid,ppid,start_time,rss,cpu_seconds,cpu_percent,cmdline,exe,role,agent,session_id,cwd,tty)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(machine_id,pid) DO UPDATE SET ppid=excluded.ppid,start_time=excluded.start_time,
		rss=excluded.rss,cpu_seconds=excluded.cpu_seconds,cpu_percent=excluded.cpu_percent,cmdline=excluded.cmdline,exe=excluded.exe,
		role=excluded.role,agent=excluded.agent,session_id=excluded.session_id,cwd=excluded.cwd,tty=excluded.tty`, tx.machineID, p.PID, p.PPID, unix(p.StartTime), p.RSS, p.CPUSeconds, p.CPUPercent, p.Cmdline, p.Exe, p.Role, p.Agent, p.SessionID, p.Cwd, p.TTY); err != nil {
			return err
		}
	}
	for pid := range old {
		if err := tx.exec("DELETE FROM procs WHERE machine_id=? AND pid=?", tx.machineID, pid); err != nil {
			return err
		}
	}
	return nil
}

// sameStoredProc compares the columns ReplaceProcs writes.
func sameStoredProc(a, b model.Proc) bool {
	return a.PPID == b.PPID && unix(a.StartTime) == unix(b.StartTime) && a.RSS == b.RSS && a.CPUSeconds == b.CPUSeconds &&
		a.CPUPercent == b.CPUPercent && a.Cmdline == b.Cmdline && a.Exe == b.Exe && a.Role == b.Role && a.Agent == b.Agent &&
		a.SessionID == b.SessionID && a.Cwd == b.Cwd && a.TTY == b.TTY
}

// Procs returns the latest processes in PID order.
func (q *querier) Procs() ([]model.Proc, error) {
	return q.ProcsScoped(q.localScope())
}

// ProcsScoped returns selected machines' processes in PID and machine order.
func (q *querier) ProcsScoped(scope Scope) ([]model.Proc, error) {
	where, args := scope.where("machine_id")
	return readMany(q, "SELECT machine_id,pid,ppid,start_time,rss,cpu_seconds,cpu_percent,cmdline,exe,role,agent,session_id,cwd,tty FROM procs WHERE "+where+" ORDER BY pid,machine_id", args, func(s scanner) (model.Proc, error) {
		var p model.Proc
		var start int64
		err := s.Scan(&p.MachineID, &p.PID, &p.PPID, &start, &p.RSS, &p.CPUSeconds, &p.CPUPercent, &p.Cmdline, &p.Exe, &p.Role, &p.Agent, &p.SessionID, &p.Cwd, &p.TTY)
		p.StartTime = fromUnix(start)
		return p, err
	})
}

const sampleColumns = "ts,mem_used,mem_compressed,mem_total,swap_used,swap_total,pressure_free,load1,load5,load15,disk_free,cores,psi_mem_some10"

// InsertSample records a machine-health reading, replacing the same timestamp.
func (tx *Tx) InsertSample(s model.MachineSample) error {
	return tx.exec(`INSERT INTO machine_samples (machine_id,`+sampleColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(machine_id,ts) DO UPDATE SET mem_used=excluded.mem_used,mem_compressed=excluded.mem_compressed,
	mem_total=excluded.mem_total,swap_used=excluded.swap_used,swap_total=excluded.swap_total,pressure_free=excluded.pressure_free,
	load1=excluded.load1,load5=excluded.load5,load15=excluded.load15,disk_free=excluded.disk_free,cores=excluded.cores,psi_mem_some10=excluded.psi_mem_some10`, tx.machineID, unix(s.TS), s.MemUsed, s.MemCompressed, s.MemTotal, s.SwapUsed, s.SwapTotal, s.PressureFree, s.Load1, s.Load5, s.Load15, s.DiskFree, s.Cores, s.PsiMemSome10)
}

func scanSample(s scanner) (model.MachineSample, error) {
	var m model.MachineSample
	var ts int64
	err := s.Scan(&m.MachineID, &ts, &m.MemUsed, &m.MemCompressed, &m.MemTotal, &m.SwapUsed, &m.SwapTotal, &m.PressureFree, &m.Load1, &m.Load5, &m.Load15, &m.DiskFree, &m.Cores, &m.PsiMemSome10)
	m.TS = fromUnix(ts)
	return m, err
}

// LatestSample returns the newest machine sample, or ErrNotFound.
func (q *querier) LatestSample() (model.MachineSample, error) {
	return q.LatestSampleFor(q.machineID)
}

// LatestSampleFor returns a machine's newest sample, or ErrNotFound.
func (q *querier) LatestSampleFor(machineID string) (model.MachineSample, error) {
	return scanSample(q.conn.QueryRowContext(q.ctx, "SELECT machine_id,"+sampleColumns+" FROM machine_samples WHERE machine_id=? ORDER BY ts DESC LIMIT 1", machineID))
}

// Samples returns readings at or after since, oldest first.
func (q *querier) Samples(since time.Time) ([]model.MachineSample, error) {
	return readMany(q, "SELECT machine_id,"+sampleColumns+" FROM machine_samples WHERE machine_id=? AND ts>=? ORDER BY ts", []any{q.machineID, unix(since)}, scanSample)
}

// UpsertRepo creates or updates a repo identified by path.
func (tx *Tx) UpsertRepo(r model.Repo) (int64, error) {
	r.RemoteURL = feed.StripURLCredentials(r.RemoteURL)
	return tx.upsertID("repos", "machine_id,path,name,remote_url", "machine_id,path", tx.machineID, r.Path, r.Name, r.RemoteURL)
}

// Repos returns repositories ordered by path.
func (q *querier) Repos() ([]model.Repo, error) {
	return q.ReposScoped(q.localScope())
}

// ReposScoped returns selected machines' repositories in path and machine order.
func (q *querier) ReposScoped(scope Scope) ([]model.Repo, error) {
	where, args := scope.where("machine_id")
	return readMany(q, "SELECT machine_id,id,path,name,remote_url FROM repos WHERE "+where+" ORDER BY path,machine_id", args, func(s scanner) (model.Repo, error) {
		var r model.Repo
		err := s.Scan(&r.MachineID, &r.ID, &r.Path, &r.Name, &r.RemoteURL)
		return r, err
	})
}

const worktreeColumns = "repo_id,path,branch,head,ahead,behind,dirty,untracked,oldest_dirty_mtime,last_commit_at,last_commit_subject,updated_at"

// UpsertWorktree creates or updates a checkout identified by path.
func (tx *Tx) UpsertWorktree(w model.Worktree) (int64, error) {
	return tx.upsertID("worktrees", "machine_id,"+worktreeColumns, "machine_id,path", tx.machineID, w.RepoID, w.Path, w.Branch, w.Head, w.Ahead, w.Behind, w.Dirty, w.Untracked, unix(w.OldestDirtyMtime), unix(w.LastCommitAt), w.LastCommitSubject, unix(w.UpdatedAt))
}

// Worktrees returns a repo's checkouts ordered by path; repoID zero selects all.
func (q *querier) Worktrees(repoID int64) ([]model.Worktree, error) {
	return readMany(q, "SELECT machine_id,id,"+worktreeColumns+" FROM worktrees WHERE machine_id=? AND (?=0 OR repo_id=?) ORDER BY path", []any{q.machineID, repoID, repoID}, func(s scanner) (model.Worktree, error) {
		var w model.Worktree
		var dirty, commit, updated int64
		err := s.Scan(&w.MachineID, &w.ID, &w.RepoID, &w.Path, &w.Branch, &w.Head, &w.Ahead, &w.Behind, &w.Dirty, &w.Untracked, &dirty, &commit, &w.LastCommitSubject, &updated)
		w.OldestDirtyMtime, w.LastCommitAt, w.UpdatedAt = fromUnix(dirty), fromUnix(commit), fromUnix(updated)
		return w, err
	})
}

const sessionColumns = "machine_id,adapter,external_id,parent_session_id,origin,version,pid,proc_start,cwd,branch,repo_id,worktree_id,title,last_prompt,registry_status,state,state_heuristic,evidence,started_at,last_activity_at,last_prompt_at,hosted,turn_open,ended_at,source_path,mixer"

// UpsertSession stores every scalar observation and derived session field.
// Completions and FilesTouched are persisted separately by the collector.
func (tx *Tx) UpsertSession(s model.Session) (int64, error) {
	mixer, err := json.Marshal(s.Mixer)
	if err != nil {
		return 0, err
	}
	return tx.upsertID("sessions", sessionColumns, "machine_id,adapter,external_id", tx.machineID, s.Adapter, s.ExternalID, s.ParentID, s.Origin, s.Version, s.PID, unix(s.ProcStart), s.Cwd, s.Branch, s.RepoID, s.WorktreeID, s.Title, s.LastPrompt, s.RegistryStatus, s.Label, s.Heuristic, s.Evidence, unix(s.StartedAt), unix(s.LastActivityAt), unix(s.LastPromptAt), s.Hosted, s.TurnOpen, nullableTime(s.EndedAt), s.SourcePath, string(mixer))
}

func scanSession(s scanner) (model.Session, error) {
	var m model.Session
	var start, procStart, activity, promptAt int64
	var ended sql.NullInt64
	var mixer string
	err := s.Scan(&m.ID, &m.MachineID, &m.Adapter, &m.ExternalID, &m.ParentID, &m.Origin, &m.Version, &m.PID, &procStart, &m.Cwd, &m.Branch, &m.RepoID, &m.WorktreeID, &m.Title, &m.LastPrompt, &m.RegistryStatus, &m.Label, &m.Heuristic, &m.Evidence, &start, &activity, &promptAt, &m.Hosted, &m.TurnOpen, &ended, &m.SourcePath, &mixer)
	if err == nil {
		err = json.Unmarshal([]byte(mixer), &m.Mixer)
	}
	m.ProcStart, m.StartedAt, m.LastActivityAt, m.LastPromptAt, m.EndedAt = fromUnix(procStart), fromUnix(start), fromUnix(activity), fromUnix(promptAt), timePointer(ended)
	return m, err
}

// Session returns a session by internal ID, or ErrNotFound.
func (q *querier) Session(id int64) (model.Session, error) {
	return scanSession(q.conn.QueryRowContext(q.ctx, "SELECT id,"+sessionColumns+" FROM sessions WHERE machine_id=? AND id=?", q.machineID, id))
}

// Sessions returns sessions matching filter, newest activity first.
func (q *querier) Sessions(filter SessionFilter) ([]model.Session, error) {
	return q.SessionsScoped(filter, q.localScope())
}

// SessionsScoped returns selected machines' sessions matching filter.
func (q *querier) SessionsScoped(filter SessionFilter, scope Scope) ([]model.Session, error) {
	where, args := scope.where("sessions.machine_id")
	query := "SELECT id," + sessionColumns + " FROM sessions WHERE " + where
	if !filter.IncludeEnded {
		if filter.EndedSince.IsZero() {
			query += " AND ended_at IS NULL"
		} else {
			query += " AND (ended_at IS NULL OR ended_at>=?)"
			args = append(args, unix(filter.EndedSince))
		}
	}
	if filter.RepoPath != "" {
		query += " AND repo_id IN (SELECT id FROM repos WHERE machine_id=sessions.machine_id AND path=?)"
		args = append(args, filter.RepoPath)
	}
	return readMany(q, query+" ORDER BY last_activity_at DESC,id", args, scanSession)
}

// ReplaceSessionFiles merges adapter deltas, preserving each path's latest edit.
// An empty delta leaves existing paths intact.
func (tx *Tx) ReplaceSessionFiles(sessionID int64, files []model.FileTouch) error {
	for _, f := range files {
		if err := tx.exec(`INSERT INTO session_files (machine_id,session_id,path,last_edit_at) VALUES (?,?,?,?)
		ON CONFLICT(machine_id,session_id,path) DO UPDATE SET last_edit_at=MAX(last_edit_at,excluded.last_edit_at)`, tx.machineID, sessionID, f.Path, unix(f.TS)); err != nil {
			return err
		}
	}
	return nil
}

// SessionFiles groups paths edited at or after since by session, in path order.
func (q *querier) SessionFiles(since time.Time) (map[int64][]model.FileTouch, error) {
	rows, err := q.conn.QueryContext(q.ctx, "SELECT machine_id,session_id,path,last_edit_at FROM session_files WHERE machine_id=? AND last_edit_at>=? ORDER BY session_id,path", q.machineID, unix(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[int64][]model.FileTouch)
	for rows.Next() {
		var id, ts int64
		var f model.FileTouch
		if err := rows.Scan(&f.MachineID, &id, &f.Path, &ts); err != nil {
			return nil, err
		}
		f.TS = fromUnix(ts)
		result[id] = append(result[id], f)
	}
	return result, rows.Err()
}

// UpsertInboxItem keeps one active wait per session. Completion IDs stay insert-only.
func (tx *Tx) UpsertInboxItem(i model.InboxItem) error {
	if i.Kind == "waiting" {
		// Notification timestamps are aliases of the active wait's natural ID.
		// Persist aliases so replayed CLI generations cannot undo an ack/end.
		if tx.machineID == tx.localID {
			alias, err := tx.Meta("inbox_alias:" + i.ID)
			if err == nil {
				i.ID = alias
			} else if !errors.Is(err, ErrNotFound) {
				return err
			}
		}
		if i.WaitingSinceAt.IsZero() {
			i.WaitingSinceAt = i.TS
		}
		if i.Reason == "" {
			i.Reason = "unknown"
		}
		if i.Source == "" {
			i.Source = "hook"
		}
		// Replay must never re-open an acknowledged or ended generation.
		var exists bool
		if err := tx.conn.QueryRowContext(tx.ctx, "SELECT EXISTS(SELECT 1 FROM inbox_items WHERE machine_id=? AND id=?)", tx.machineID, i.ID).Scan(&exists); err != nil {
			return err
		}
		if exists {
			var resolved bool
			if err := tx.conn.QueryRowContext(tx.ctx, "SELECT acked_at IS NOT NULL OR ended_at IS NOT NULL FROM inbox_items WHERE machine_id=? AND id=?", tx.machineID, i.ID).Scan(&resolved); err != nil {
				return err
			}
			if resolved {
				return nil
			}
		}
		active, err := tx.Inbox(time.Time{}, false)
		if err != nil {
			return err
		}
		for _, old := range active {
			if old.Kind != "waiting" || old.SessionID != i.SessionID {
				continue
			}
			if i.TS.Before(old.TS) {
				return nil
			}
			fromCLI := i.Source == "agents_cli"
			var hookReason, agentsReason string
			if err := tx.conn.QueryRowContext(tx.ctx, "SELECT hook_reason,agents_reason FROM inbox_items WHERE machine_id=? AND id=?", tx.machineID, old.ID).Scan(&hookReason, &agentsReason); err != nil {
				return err
			}
			if fromCLI {
				agentsReason = i.Reason
			} else {
				hookReason = i.Reason
			}
			i.Reason = hookReason
			if i.Reason == "" || i.Reason == "unknown" {
				i.Reason = agentsReason
			}
			if i.Reason == "" {
				i.Reason = "unknown"
			}
			if fromCLI && (old.Source == "hook" || old.Source == "both") {
				i.Tool, i.Detail, i.Summary = old.Tool, old.Detail, old.Summary
			}
			if old.Source != i.Source {
				i.Source = "both"
			}
			if i.ID != old.ID && tx.machineID == tx.localID {
				if err := tx.SetMeta("inbox_alias:"+i.ID, old.ID); err != nil {
					return err
				}
			}
			if i.Jump == nil {
				i.Jump = old.Jump
			}
			jump, err := json.Marshal(i.Jump)
			if err != nil {
				return err
			}
			return tx.exec("UPDATE inbox_items SET reason=?,tool=?,detail=?,summary=?,source=?,jump=?,hook_reason=?,agents_reason=? WHERE machine_id=? AND id=?", i.Reason, i.Tool, i.Detail, i.Summary, i.Source, string(jump), hookReason, agentsReason, tx.machineID, old.ID)
		}
	}
	jump, err := json.Marshal(i.Jump)
	if err != nil {
		return err
	}
	hookReason, agentsReason := "", ""
	if i.Source == "hook" || i.Source == "both" {
		hookReason = i.Reason
	}
	if i.Source == "agents_cli" || i.Source == "both" {
		agentsReason = i.Reason
	}
	return tx.exec("INSERT OR IGNORE INTO inbox_items (machine_id,id,session_id,kind,ts,summary,acked_at,ack_pending_at,reason,tool,detail,waiting_since_at,source,ended_at,jump,hook_reason,agents_reason) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", tx.machineID, i.ID, i.SessionID, i.Kind, unix(i.TS), i.Summary, nullableTime(i.AckedAt), nullableTime(i.AckPendingAt), i.Reason, i.Tool, i.Detail, unix(i.WaitingSinceAt), i.Source, nullableTime(i.EndedAt), string(jump), hookReason, agentsReason)
}

// AckInbox acknowledges an existing unacknowledged item, returning whether it changed.
func (tx *Tx) AckInbox(id string, at time.Time) (bool, error) {
	r, err := tx.conn.ExecContext(tx.ctx, "UPDATE inbox_items SET acked_at=?,ack_pending_at=NULL WHERE machine_id=? AND id=? AND acked_at IS NULL", unix(at), tx.machineID, id)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

// Inbox returns active waits oldest first, then completions newest first.
// includeAcked also includes automatically ended history.
func (q *querier) Inbox(since time.Time, includeAcked bool) ([]model.InboxItem, error) {
	return q.InboxScoped(since, includeAcked, q.localScope())
}

// InboxScoped applies the same attention ordering across selected machines.
func (q *querier) InboxScoped(since time.Time, includeAcked bool, scope Scope) ([]model.InboxItem, error) {
	where, args := scope.where("machine_id")
	args = append(args, unix(since), includeAcked)
	return readMany(q, "SELECT machine_id,id,session_id,kind,ts,summary,acked_at,ack_pending_at,reason,tool,detail,waiting_since_at,source,ended_at,jump FROM inbox_items WHERE "+where+" AND ts>=? AND (? OR (acked_at IS NULL AND ended_at IS NULL)) ORDER BY CASE WHEN kind='waiting' AND acked_at IS NULL AND ended_at IS NULL THEN 0 ELSE 1 END, CASE WHEN kind='waiting' AND acked_at IS NULL AND ended_at IS NULL THEN ts END ASC,ts DESC,id,machine_id", args, func(s scanner) (model.InboxItem, error) {
		var i model.InboxItem
		var ts, wait int64
		var ack, pending, ended sql.NullInt64
		var jump string
		err := s.Scan(&i.MachineID, &i.ID, &i.SessionID, &i.Kind, &ts, &i.Summary, &ack, &pending, &i.Reason, &i.Tool, &i.Detail, &wait, &i.Source, &ended, &jump)
		i.TS, i.AckedAt, i.AckPendingAt = fromUnix(ts), timePointer(ack), timePointer(pending)
		i.WaitingSinceAt, i.EndedAt = fromUnix(wait), timePointer(ended)
		if err == nil {
			err = json.Unmarshal([]byte(jump), &i.Jump)
		}
		return i, err
	})
}

// ReplaceOrphans replaces this machine's latest orphan snapshot.
func (tx *Tx) ReplaceOrphans(orphans []model.Orphan) error {
	if err := tx.exec("DELETE FROM orphans WHERE machine_id=?", tx.machineID); err != nil {
		return err
	}
	for _, o := range orphans {
		evidence, err := json.Marshal(o.Evidence)
		if err != nil {
			return err
		}
		pids, err := json.Marshal(o.PIDs)
		if err != nil {
			return err
		}
		if err := tx.exec(`INSERT INTO orphans (machine_id,id,root_pid,start_time,signature,proc_count,rss_total,cwd,young,verified,evidence,pids,first_seen)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`, tx.machineID, o.ID, o.RootPID, unix(o.StartTime), o.Signature, o.ProcCount, o.RSSTotal, o.Cwd, o.Young, o.Verified, string(evidence), string(pids), unix(o.FirstSeen)); err != nil {
			return err
		}
	}
	return nil
}

// Orphans returns orphan process trees ordered by ID.
func (q *querier) Orphans() ([]model.Orphan, error) {
	return q.OrphansScoped(q.localScope())
}

// OrphansScoped returns selected machines' orphan trees in ID and machine order.
func (q *querier) OrphansScoped(scope Scope) ([]model.Orphan, error) {
	where, args := scope.where("machine_id")
	return readMany(q, "SELECT machine_id,id,root_pid,start_time,signature,proc_count,rss_total,cwd,young,verified,evidence,pids,first_seen FROM orphans WHERE "+where+" ORDER BY id,machine_id", args, func(s scanner) (model.Orphan, error) {
		var o model.Orphan
		var start, first int64
		var evidence, pids string
		if err := s.Scan(&o.MachineID, &o.ID, &o.RootPID, &start, &o.Signature, &o.ProcCount, &o.RSSTotal, &o.Cwd, &o.Young, &o.Verified, &evidence, &pids, &first); err != nil {
			return o, err
		}
		o.StartTime, o.FirstSeen = fromUnix(start), fromUnix(first)
		if err := json.Unmarshal([]byte(evidence), &o.Evidence); err != nil {
			return o, err
		}
		err := json.Unmarshal([]byte(pids), &o.PIDs)
		return o, err
	})
}

// SetSourceOK records a successful collection, retaining the last error history.
func (tx *Tx) SetSourceOK(name string, at time.Time) error {
	return tx.exec(`INSERT INTO sources (machine_id,name,last_ok_at) VALUES (?,?,?)
	ON CONFLICT(machine_id,name) DO UPDATE SET last_ok_at=excluded.last_ok_at`, tx.machineID, name, unix(at))
}

// SetSourcePending records an enabled event source waiting for its first input.
// A listener starting is not a successful read; clear previous-run diagnostics.
func (tx *Tx) SetSourcePending(name string) error {
	return tx.exec(`INSERT INTO sources (machine_id,name,last_ok_at,last_error,last_error_at) VALUES (?,?,0,'',0)
 ON CONFLICT(machine_id,name) DO UPDATE SET last_ok_at=0,last_error='',last_error_at=0`, tx.machineID, name)
}

// SetSourceError records a source's last error without changing its last success.
func (tx *Tx) SetSourceError(name, message string, at time.Time) error {
	return tx.exec(`INSERT INTO sources (machine_id,name,last_error,last_error_at) VALUES (?,?,?,?)
	ON CONFLICT(machine_id,name) DO UPDATE SET last_error=excluded.last_error,last_error_at=excluded.last_error_at`, tx.machineID, name, message, unix(at))
}

// Sources returns collection source freshness, ordered by name.
func (q *querier) Sources() ([]model.SourceStatus, error) {
	return readMany(q, "SELECT machine_id,name,last_ok_at,last_error,last_error_at FROM sources WHERE machine_id=? ORDER BY name", []any{q.machineID}, func(s scanner) (model.SourceStatus, error) {
		var m model.SourceStatus
		var ok, failed int64
		err := s.Scan(&m.MachineID, &m.Name, &ok, &m.LastError, &failed)
		m.LastOKAt, m.LastErrorAt = fromUnix(ok), fromUnix(failed)
		return m, err
	})
}

// InsertGap records a collector downtime interval once.
func (tx *Tx) InsertGap(g model.Gap) error {
	r, err := tx.conn.ExecContext(tx.ctx, "INSERT OR IGNORE INTO gaps (machine_id,from_ts,to_ts) VALUES (?,?,?)", tx.machineID, unix(g.From), unix(g.To))
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil || n == 0 || tx.machineID != tx.localID {
		return err
	}
	g.MachineID = tx.machineID
	key, payload := feed.Encode(feed.Gap, g, feed.None)
	_, err = tx.AppendFeed(string(feed.Gap), key, payload, g.To)
	return err
}

// Gaps returns intervals ending at or after since, oldest first.
func (q *querier) Gaps(since time.Time) ([]model.Gap, error) {
	return readMany(q, "SELECT machine_id,from_ts,to_ts FROM gaps WHERE machine_id=? AND to_ts>=? ORDER BY from_ts,to_ts", []any{q.machineID, unix(since)}, func(s scanner) (model.Gap, error) {
		var from, to int64
		var machine string
		err := s.Scan(&machine, &from, &to)
		return model.Gap{MachineID: machine, From: fromUnix(from), To: fromUnix(to)}, err
	})
}

// GetOffset returns a saved tail offset and whether it exists.
func (q *querier) GetOffset(path string) (model.Offset, bool, error) {
	var o model.Offset
	var mtime int64
	err := q.conn.QueryRowContext(q.ctx, "SELECT machine_id,inode,size,offset,mtime FROM offsets WHERE machine_id=? AND path=?", q.machineID, path).Scan(&o.MachineID, &o.Inode, &o.Size, &o.Pos, &mtime)
	if err == sql.ErrNoRows {
		return o, false, nil
	}
	o.Mtime = fromUnix(mtime)
	return o, err == nil, err
}

// SetOffset saves a file's incremental read position.
func (tx *Tx) SetOffset(path string, o model.Offset) error {
	return tx.exec(`INSERT INTO offsets (machine_id,path,inode,size,offset,mtime) VALUES (?,?,?,?,?,?)
	ON CONFLICT(machine_id,path) DO UPDATE SET inode=excluded.inode,size=excluded.size,offset=excluded.offset,mtime=excluded.mtime`, tx.machineID, path, o.Inode, o.Size, o.Pos, unix(o.Mtime))
}

// Meta returns a stored metadata value, or ErrNotFound.
func (q *querier) Meta(key string) (string, error) {
	var value string
	err := q.conn.QueryRowContext(q.ctx, "SELECT value FROM meta WHERE machine_id=? AND key=?", q.machineID, key).Scan(&value)
	return value, err
}

// SetMeta stores metadata. The machine identity is immutable after creation.
// It is available on Store as well as Tx; use Tx inside a transaction callback.
func (q *querier) SetMeta(key, value string) error {
	if q.machineID != q.localID {
		return fmt.Errorf("metadata is local-only")
	}
	if key == "machine_id" && value != q.machineID {
		return fmt.Errorf("machine_id is immutable")
	}
	return q.exec("INSERT INTO meta (machine_id,key,value) VALUES (?,?,?) ON CONFLICT(key) DO UPDATE SET machine_id=excluded.machine_id,value=excluded.value", q.machineID, key, value)
}

// InsertEvent records an event and keeps the newest 200 for that adapter/session.
func (tx *Tx) InsertEvent(e model.Event) error {
	metadata, err := json.Marshal(e.Metadata)
	if err != nil {
		return err
	}
	if err := tx.exec("INSERT INTO events (machine_id,adapter,external_id,ts,kind,summary,metadata) VALUES (?,?,?,?,?,?,?)", tx.machineID, e.Adapter, e.ExternalID, unix(e.TS), e.Kind, e.Summary, string(metadata)); err != nil {
		return err
	}
	if err := tx.exec(`INSERT INTO event_minutes(machine_id,minute,count) VALUES(?,?,1)
 ON CONFLICT(machine_id,minute) DO UPDATE SET count=count+1`, tx.machineID, unix(e.TS.Truncate(time.Minute))); err != nil {
		return err
	}
	return tx.exec(`DELETE FROM events WHERE machine_id=? AND adapter=? AND external_id=? AND id NOT IN
	(SELECT id FROM events WHERE machine_id=? AND adapter=? AND external_id=? ORDER BY ts DESC,id DESC LIMIT 200)`, tx.machineID, e.Adapter, e.ExternalID, tx.machineID, e.Adapter, e.ExternalID)
}

func scanEvent(s scanner) (model.Event, error) {
	var e model.Event
	var ts int64
	var metadata string
	err := s.Scan(&e.MachineID, &e.Adapter, &e.ExternalID, &ts, &e.Kind, &e.Summary, &metadata)
	if err == nil {
		err = json.Unmarshal([]byte(metadata), &e.Metadata)
	}
	e.TS = fromUnix(ts)
	return e, err
}

// Events returns a session's events at or after since, oldest first.
func (q *querier) Events(adapter, externalID string, since time.Time) ([]model.Event, error) {
	return readMany(q, "SELECT machine_id,adapter,external_id,ts,kind,summary,metadata FROM events WHERE machine_id=? AND adapter=? AND external_id=? AND ts>=? ORDER BY ts,id", []any{q.machineID, adapter, externalID, unix(since)}, scanEvent)
}

// RecentEvents reads a bounded tail, newest first, including equal timestamps.
func (q *querier) RecentEvents(until time.Time, limit int) ([]model.Event, error) {
	limit = max(0, min(limit, 200))
	return readMany(q, "SELECT machine_id,adapter,external_id,ts,kind,summary,metadata FROM events WHERE machine_id=? AND ts<=? ORDER BY ts DESC,id DESC LIMIT ?", []any{q.machineID, unix(until), limit}, scanEvent)
}

// LatestEvents returns events across sessions at or after since, newest first.
func (q *querier) LatestEvents(since time.Time) ([]model.Event, error) {
	return readMany(q, "SELECT machine_id,adapter,external_id,ts,kind,summary,metadata FROM events WHERE machine_id=? AND ts>=? ORDER BY ts DESC,id DESC", []any{q.machineID, unix(since)}, scanEvent)
}

// InsertHandoff records a session's completion text.
func (tx *Tx) InsertHandoff(sessionID int64, ts time.Time, text string) error {
	return tx.exec("INSERT INTO handoffs (machine_id,session_id,ts,text) VALUES (?,?,?,?)", tx.machineID, sessionID, unix(ts), text)
}

// Handoffs returns a repo's newest completions, up to limit (all if limit <= 0).
// Completion IDs are the decimal handoff row IDs.
func (q *querier) Handoffs(repoID int64, limit int) ([]model.CompletionEvent, error) {
	if limit <= 0 {
		limit = -1
	}
	return readMany(q, `SELECT h.machine_id,h.id,h.ts,h.text FROM handoffs h JOIN sessions s ON s.id=h.session_id AND s.machine_id=h.machine_id
	WHERE h.machine_id=? AND s.repo_id=? ORDER BY h.ts DESC,h.id DESC LIMIT ?`, []any{q.machineID, repoID, limit}, func(s scanner) (model.CompletionEvent, error) {
		var e model.CompletionEvent
		var id, ts int64
		err := s.Scan(&e.MachineID, &id, &ts, &e.Text)
		e.ID, e.TS = strconv.FormatInt(id, 10), fromUnix(ts)
		return e, err
	})
}

// Prune removes expired history and keeps the latest sample in each five-minute
// bucket between 24 hours and seven days old. Samples in the last day stay intact.
func (tx *Tx) Prune(now time.Time) error {
	month, week, day := unix(now.Add(-30*24*time.Hour)), unix(now.Add(-7*24*time.Hour)), unix(now.Add(-24*time.Hour))
	// Inbox has no cascading FK; events reference the natural key. Keep the
	// attribution row for every retained reference, on local and remote machines.
	const expiredSession = `machine_id=? AND ended_at<?
	AND NOT EXISTS (SELECT 1 FROM inbox_items i WHERE i.machine_id=sessions.machine_id AND i.session_id=sessions.id AND i.ts>=?)
	AND NOT EXISTS (SELECT 1 FROM events e WHERE e.machine_id=sessions.machine_id AND e.adapter=sessions.adapter AND e.external_id=sessions.external_id AND e.ts>=?)
	AND NOT EXISTS (SELECT 1 FROM handoffs h WHERE h.machine_id=sessions.machine_id AND h.session_id=sessions.id AND h.ts>=?)`
	sessionArgs := []any{tx.machineID, month, month, month, month}
	// Capture natural keys before deleting rows (including cascading children).
	if tx.machineID == tx.localID {
		sessions, err := readMany(&tx.querier, "SELECT id,"+sessionColumns+" FROM sessions WHERE "+expiredSession, sessionArgs, scanSession)
		if err != nil {
			return err
		}
		events, err := tx.LatestEvents(time.Time{})
		if err != nil {
			return err
		}
		items, err := tx.Inbox(time.Time{}, true)
		if err != nil {
			return err
		}
		remove := func(kind feed.Kind, row any) error {
			key, _ := feed.Encode(kind, row, feed.None)
			deleteKey, payload := feed.Encode(feed.Delete, feed.DeleteRow{Kind: kind, Key: key}, feed.None)
			_, err := tx.AppendFeed(string(feed.Delete), deleteKey, payload, now)
			return err
		}
		for _, s := range sessions {
			if s.EndedAt != nil && unix(*s.EndedAt) < month {
				if err := remove(feed.Session, s); err != nil {
					return err
				}
			}
		}
		for _, e := range events {
			if unix(e.TS) < month {
				if err := remove(feed.Event, e); err != nil {
					return err
				}
			}
		}
		for _, i := range items {
			if unix(i.TS) < month {
				if err := remove(feed.Inbox, i); err != nil {
					return err
				}
			}
		}
	}
	for _, stmt := range []struct {
		query string
		args  []any
	}{
		{"DELETE FROM events WHERE machine_id=? AND ts<?", []any{tx.machineID, month}},
		{"DELETE FROM event_minutes WHERE machine_id=? AND minute<?", []any{tx.machineID, unix(now.Add(-30 * 24 * time.Hour).Truncate(time.Minute))}},
		{"DELETE FROM observation_minutes WHERE machine_id=? AND minute<?", []any{tx.machineID, now.Add(-30 * 24 * time.Hour).Truncate(time.Minute).UnixMilli()}},
		{"DELETE FROM inbox_items WHERE machine_id=? AND ts<?", []any{tx.machineID, month}},
		{"DELETE FROM handoffs WHERE machine_id=? AND ts<?", []any{tx.machineID, month}},
		{"DELETE FROM sessions WHERE " + expiredSession, sessionArgs},
		{"DELETE FROM machine_samples WHERE machine_id=? AND ts<?", []any{tx.machineID, week}},
		{`DELETE FROM machine_samples WHERE machine_id=? AND ts<? AND ts NOT IN
		(SELECT MAX(ts) FROM machine_samples WHERE machine_id=? AND ts>=? AND ts<? GROUP BY ts/?)`, []any{tx.machineID, day, tx.machineID, week, day, int64(5 * time.Minute)}},
		{"DELETE FROM gaps WHERE machine_id=? AND to_ts<?", []any{tx.machineID, month}},
	} {
		if err := tx.exec(stmt.query, stmt.args...); err != nil {
			return err
		}
	}
	if tx.machineID == tx.localID {
		// Remove a sequence prefix even if the wall clock moved backwards, so
		// min(seq)-1 remains a sufficient guard against skipping deleted rows.
		if err := tx.exec(`DELETE FROM feed WHERE seq<=(SELECT MAX(seq) FROM feed WHERE ts<?) OR seq IN
		(SELECT seq FROM feed ORDER BY seq DESC LIMIT -1 OFFSET 200000)`, month); err != nil {
			return err
		}
		if err := tx.exec(`DELETE FROM meta WHERE machine_id=? AND key LIKE 'inbox_alias:%'
 AND NOT EXISTS(SELECT 1 FROM inbox_items i WHERE i.machine_id=? AND i.id=meta.value)`, tx.machineID, tx.machineID); err != nil {
			return err
		}
		if err := tx.exec(`DELETE FROM meta WHERE machine_id=? AND (key LIKE 'inbox_alert:%' OR key LIKE 'inbox_alert_session:%') AND value<?`, tx.machineID, now.Add(-30*24*time.Hour).UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		return tx.PruneRemote(now)
	}
	return nil
}

// FeedRow is one committed fleet change, ordered by its persistent sequence.
type FeedRow = feed.FeedRow

// ErrResnapshot means the epoch or cursor cannot be resumed safely.
var ErrResnapshot = errors.New("feed cursor requires resnapshot")

// FeedSince validates and reads a cursor in one read transaction, so a concurrent
// retention pass cannot prune rows between checking bounds and reading them.
func (s *Store) FeedSince(epoch string, seq int64, limit int) ([]FeedRow, error) {
	tx, err := s.db.BeginTx(s.ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := s.querier
	q.conn = tx
	rows, err := q.FeedSince(epoch, seq, limit)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return rows, nil
}

// AppendFeed appends a local change in the same transaction as its data writes.
func (tx *Tx) AppendFeed(kind, key string, payload []byte, ts time.Time) (int64, error) {
	if tx.machineID != tx.localID {
		return 0, fmt.Errorf("feed is local-only")
	}
	if enabled, err := tx.Meta("feed_enabled"); err != nil && !errors.Is(err, ErrNotFound) {
		return 0, err
	} else if enabled == "false" {
		return 0, nil
	}
	if !json.Valid(payload) {
		return 0, fmt.Errorf("invalid feed payload for %s", kind)
	}
	var seq int64
	err := tx.conn.QueryRowContext(tx.ctx, "INSERT INTO feed (ts,kind,key,payload) VALUES (?,?,?,?) RETURNING seq", unix(ts), kind, key, string(payload)).Scan(&seq)
	return seq, err
}

// ConfigureFeed suspends publication without consumers. Restarting publication
// rotates the epoch because changes during suspension require a fresh snapshot.
func (tx *Tx) ConfigureFeed(enabled bool) error {
	previous, err := tx.Meta("feed_enabled")
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	value := strconv.FormatBool(enabled)
	if previous == value {
		return nil
	}
	if previous != "" || !enabled {
		if err := tx.resetFeed(); err != nil {
			return err
		}
	}
	return tx.SetMeta("feed_enabled", value)
}

// SetFeedShare persists the effective export policy. A stricter policy removes
// retained text and invalidates every old cursor in the same transaction.
func (tx *Tx) SetFeedShare(share feed.ShareText) error {
	previous, err := tx.Meta("feed_share_text")
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	level := map[feed.ShareText]int{feed.None: 0, feed.Titles: 1, feed.Snippets: 2}
	// An unversioned retained feed may contain snippets from an older collector.
	oldLevel := 2
	if previous != "" {
		oldLevel = level[feed.ShareText(previous)]
	}
	if level[share] < oldLevel {
		if err := tx.resetFeed(); err != nil {
			return err
		}
	}
	if previous == string(share) {
		return nil
	}
	return tx.SetMeta("feed_share_text", string(share))
}

func (tx *Tx) resetFeed() error {
	epoch, err := randomID()
	if err != nil {
		return err
	}
	if err := tx.exec("DELETE FROM feed"); err != nil {
		return err
	}
	return tx.SetMeta("feed_epoch", epoch)
}

// FeedBounds returns the epoch and retained cursor range. With no retained rows,
// minSeq is maxSeq+1; maxSeq still remembers the last committed AUTOINCREMENT ID.
func (q *querier) FeedBounds() (epoch string, minSeq, maxSeq int64, err error) {
	err = q.conn.QueryRowContext(q.ctx, `SELECT value,
	COALESCE((SELECT MIN(seq) FROM feed),COALESCE((SELECT seq FROM sqlite_sequence WHERE name='feed'),0)+1),
	COALESCE((SELECT seq FROM sqlite_sequence WHERE name='feed'),0)
	FROM meta WHERE key='feed_epoch' AND machine_id=?`, q.localID).Scan(&epoch, &minSeq, &maxSeq)
	return
}

// FeedSince returns changes strictly after seq; nonpositive limit means all.
// Use the transaction's method when reading alongside snapshot data.
func (q *querier) FeedSince(epoch string, seq int64, limit int) ([]FeedRow, error) {
	current, min, max, err := q.FeedBounds()
	if err != nil {
		return nil, err
	}
	if epoch != current || seq < min-1 || seq > max {
		return nil, ErrResnapshot
	}
	if limit <= 0 {
		limit = -1
	}
	return readMany(q, "SELECT seq,ts,kind,key,payload FROM feed WHERE seq>? ORDER BY seq LIMIT ?", []any{seq, limit}, func(s scanner) (FeedRow, error) {
		var row FeedRow
		var ts int64
		var payload string
		err := s.Scan(&row.Seq, &ts, &row.Kind, &row.Key, &payload)
		row.TS, row.Payload = fromUnix(ts), json.RawMessage(payload)
		return row, err
	})
}

// Offsets loads tail positions once before concurrent source collection.
func (q *querier) Offsets() (map[string]model.Offset, error) {
	rows, err := q.conn.QueryContext(q.ctx, "SELECT machine_id,path,inode,size,offset,mtime FROM offsets WHERE machine_id=?", q.machineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]model.Offset)
	for rows.Next() {
		var path string
		var o model.Offset
		var mtime int64
		if err := rows.Scan(&o.MachineID, &path, &o.Inode, &o.Size, &o.Pos, &mtime); err != nil {
			return nil, err
		}
		o.Mtime = fromUnix(mtime)
		result[path] = o
	}
	return result, rows.Err()
}

// PruneSnapshot bounds disposable collection to the displayed activity window,
// while also applying normal session, inbox, handoff and file retention.
func (tx *Tx) PruneSnapshot(now time.Time) error {
	if err := tx.Prune(now); err != nil {
		return err
	}
	cutoff := unix(now.Truncate(time.Minute).Add(-59 * time.Minute))
	if err := tx.exec("DELETE FROM observation_minutes WHERE machine_id=? AND minute<?", tx.machineID, now.Truncate(time.Minute).Add(-59*time.Minute).UnixMilli()); err != nil {
		return err
	}
	for _, query := range []string{
		"DELETE FROM machine_samples WHERE machine_id=? AND ts<?",
		"DELETE FROM events WHERE machine_id=? AND ts<?",
		"DELETE FROM event_minutes WHERE machine_id=? AND minute<?",
	} {
		if err := tx.exec(query, tx.machineID, cutoff); err != nil {
			return err
		}
	}
	if err := tx.exec("DELETE FROM session_files WHERE machine_id=? AND last_edit_at<?", tx.machineID, unix(now.Add(-30*time.Minute))); err != nil {
		return err
	}
	// A disposable viewer does not publish a resumable fleet feed.
	return tx.exec("DELETE FROM feed")
}

// SessionHandoffs reads completion marks for one session, never a repo tail.
func (q *querier) SessionHandoffs(id int64, from, to time.Time) ([]model.CompletionEvent, error) {
	return readMany(q, `SELECT machine_id,id,ts,text FROM handoffs WHERE machine_id=? AND session_id=? AND ts>=? AND ts<=? ORDER BY ts,id`, []any{q.machineID, id, unix(from), unix(to)}, func(s scanner) (model.CompletionEvent, error) {
		var e model.CompletionEvent
		var id, ts int64
		err := s.Scan(&e.MachineID, &id, &ts, &e.Text)
		e.ID, e.TS = strconv.FormatInt(id, 10), fromUnix(ts)
		return e, err
	})
}
