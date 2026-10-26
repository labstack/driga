package fleet

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/labstack/driga/internal/pathx"
	"io"
	"path/filepath"
	"time"

	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/model"
)

// streamSnapshot keeps the cursor, reference closure and row encoder inside
// one SQLite WAL read transaction on a dedicated read-only connection.
func (s *Server) streamSnapshot(ctx context.Context, w io.Writer) error {
	db, err := sql.Open("sqlite", pathx.FileURL(filepath.Join(s.dataDir, "driga.db"))+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.streamSnapshotTx(ctx, tx, w); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) streamSnapshotTx(ctx context.Context, tx *sql.Tx, out io.Writer) error {
	now, local, share := s.now().UTC(), s.st.MachineID(), s.share()
	var cursor Cursor
	if err := tx.QueryRowContext(ctx, `SELECT value,COALESCE((SELECT seq FROM sqlite_sequence WHERE name='feed'),0) FROM meta WHERE key='feed_epoch'`).Scan(&cursor.Epoch, &cursor.Seq); err != nil {
		return err
	}
	var machine json.RawMessage
	if err := snapshotRows[model.Machine](ctx, tx, "machines", local, "", func(v model.Machine) error {
		_, machine = feed.Encode(feed.Machine, v, share)
		return nil
	}); err != nil {
		return err
	}
	if machine == nil {
		return fmt.Errorf("fleet snapshot: local machine missing")
	}

	// Keep identities and parent edges only, never session text or row JSON.
	keys := map[int64]feed.SessionKey{}
	parents := map[int64]int64{}
	include := map[int64]bool{}
	rows, err := tx.QueryContext(ctx, `SELECT id,adapter,external_id,parent_session_id,ended_at FROM sessions WHERE machine_id=?`, local)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var key feed.SessionKey
		var parent, ended sql.NullInt64
		if err = rows.Scan(&id, &key.Adapter, &key.ExternalID, &parent, &ended); err != nil {
			break
		}
		keys[id] = key
		if parent.Valid {
			parents[id] = parent.Int64
		}
		if !ended.Valid || ended.Int64 >= now.Add(-24*time.Hour).UnixNano() {
			include[id] = true
		}
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	// Match retention windows before following parents. UNION deduplicates
	// references without loading files/events/inbox payloads in Go.
	rows, err = tx.QueryContext(ctx, `SELECT session_id FROM session_files WHERE machine_id=? AND last_edit_at>=?
 UNION SELECT s.id FROM events e JOIN sessions s ON s.machine_id=e.machine_id AND s.adapter=e.adapter AND s.external_id=e.external_id WHERE e.machine_id=? AND e.ts>=?
 UNION SELECT s.id FROM panel_events e JOIN sessions s ON s.machine_id=e.machine_id AND s.adapter=e.adapter AND s.external_id=e.external_id WHERE e.machine_id=? AND e.ts>=?
 UNION SELECT session_id FROM inbox_items WHERE machine_id=? AND ts>=?
 UNION SELECT session_id FROM procs WHERE machine_id=? AND agent<>'' AND session_id<>0
 UNION SELECT session_id FROM handoffs WHERE machine_id=? AND ?`,
		local, now.Add(-30*time.Minute).UnixNano(), local, now.Add(-7*24*time.Hour).UnixNano(), local, now.Add(-7*24*time.Hour).UnixNano(), local, now.Add(-30*24*time.Hour).UnixNano(), local, local, share == feed.Snippets)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		include[id] = true
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	for id := range include {
		for parent, ok := parents[id]; ok && !include[parent]; parent, ok = parents[parent] {
			include[parent] = true
		}
	}

	// Buffer small punctuation writes, never a complete table or response.
	w := bufio.NewWriterSize(out, 32<<10)
	enc := json.NewEncoder(w)
	if _, err := fmt.Fprintf(w, `{"schema":2,"observed_at":%q,"mode":"daemon","data":{"machine":`, now.Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if err := enc.Encode(machine); err != nil {
		return err
	}
	if _, err := io.WriteString(w, `,"cursor":`); err != nil {
		return err
	}
	if err := enc.Encode(cursor); err != nil {
		return err
	}
	array := func(name string, run func(func([]byte) error) error) error {
		if _, err := fmt.Fprintf(w, ",%q:[", name); err != nil {
			return err
		}
		first := true
		emit := func(p []byte) error {
			if len(p) == 0 {
				return nil
			}
			if !first {
				if err := w.WriteByte(','); err != nil {
					return err
				}
			}
			first = false
			_, err := w.Write(p)
			return err
		}
		if err := run(emit); err != nil {
			return err
		}
		return w.WriteByte(']')
	}
	repoPaths, workPaths := map[int64]string{}, map[int64]string{}
	if err := array("repos", func(emit func([]byte) error) error {
		return snapshotRows[model.Repo](ctx, tx, "repos", local, "", func(v model.Repo) error {
			repoPaths[v.ID] = v.Path
			_, p := feed.Encode(feed.Repo, v, share)
			return emit(p)
		})
	}); err != nil {
		return err
	}
	if err := array("worktrees", func(emit func([]byte) error) error {
		return snapshotRows[model.Worktree](ctx, tx, "worktrees", local, "", func(v model.Worktree) error {
			workPaths[v.ID] = v.Path
			_, p := feed.Encode(feed.Worktree, feed.WorktreeRow{Worktree: v, RepoPath: repoPaths[v.RepoID]}, share)
			return emit(p)
		})
	}); err != nil {
		return err
	}
	if err := array("summaries", func(emit func([]byte) error) error {
		return snapshotRows[model.Summary](ctx, tx, "summaries", local, "", func(v model.Summary) error {
			_, p := feed.Encode(feed.Summary, feed.SummaryRow{Summary: v, RepoPath: repoPaths[v.ProjectID]}, share)
			return emit(p)
		})
	}); err != nil {
		return err
	}
	if err := array("sessions", func(emit func([]byte) error) error {
		return snapshotRows[model.Session](ctx, tx, "sessions", local, "", func(v model.Session) error {
			if !include[v.ID] {
				return nil
			}
			row := feed.SessionRow{Session: v}
			if v.ParentID != nil {
				if key, ok := keys[*v.ParentID]; ok {
					row.Parent = &key
				}
			}
			if v.RepoID != nil {
				row.RepoPath = repoPaths[*v.RepoID]
			}
			if v.WorktreeID != nil {
				row.WorktreePath = workPaths[*v.WorktreeID]
			}
			_, p := feed.Encode(feed.Session, row, share)
			return emit(p)
		})
	}); err != nil {
		return err
	}
	if err := array("session_files", func(emit func([]byte) error) error {
		// A session's file window is one wire row, not a table-sized batch.
		var id int64
		var files []model.FileTouch
		flush := func() error {
			key, ok := keys[id]
			if !ok || len(files) == 0 {
				return nil
			}
			_, p := feed.Encode(feed.SessionFiles, feed.FilesRow{MachineID: local, Session: key, Files: files}, share)
			return emit(p)
		}
		err := snapshotRows[snapshotFile](ctx, tx, "session_files", local, " AND last_edit_at>=? ORDER BY session_id", func(v snapshotFile) error {
			if v.SessionID != id {
				if err := flush(); err != nil {
					return err
				}
				files = nil
				id = v.SessionID
			}
			files = append(files, v.FileTouch)
			return nil
		}, now.Add(-30*time.Minute).UnixNano())
		if err != nil {
			return err
		}
		return flush()
	}); err != nil {
		return err
	}
	if err := array("events", func(emit func([]byte) error) error {
		encode := func(v model.Event) error { _, p := feed.Encode(feed.Event, v, share); return emit(p) }
		if err := snapshotRows[model.Event](ctx, tx, "events", local, " AND ts>=?", encode, now.Add(-7*24*time.Hour).UnixNano()); err != nil {
			return err
		}
		// Metadata older than the raw tail still belongs to a fresh peer's
		// last day/30-turn view. Raw rows already emitted above are omitted.
		return snapshotRows[model.Event](ctx, tx, "panel_events", local, ` AND ts>=? AND (NOT (kind LIKE 'log:tool_call:%' OR kind IN ('PostToolUse','PostToolUseFailure','postToolUse','postToolUseFailure','otlp:tool_result')) OR (machine_id,adapter,external_id,ts,kind) IN (
 SELECT machine_id,adapter,external_id,ts,kind FROM (
 SELECT machine_id,adapter,external_id,ts,kind, ROW_NUMBER() OVER (PARTITION BY machine_id,adapter,external_id ORDER BY ts DESC,kind DESC) AS row
 FROM panel_events WHERE machine_id=? AND ts>=? AND (kind LIKE 'log:tool_call:%' OR kind IN ('PostToolUse','PostToolUseFailure','postToolUse','postToolUseFailure','otlp:tool_result'))) WHERE row<=1000))
 AND NOT EXISTS(SELECT 1 FROM events e WHERE e.machine_id=panel_events.machine_id AND e.adapter=panel_events.adapter AND e.external_id=panel_events.external_id AND e.ts=panel_events.ts AND e.kind=panel_events.kind)`, encode, now.Add(-7*24*time.Hour).UnixNano(), local, now.Add(-7*24*time.Hour).UnixNano())
	}); err != nil {
		return err
	}
	if err := array("handoffs", func(emit func([]byte) error) error {
		if share != feed.Snippets {
			return nil
		}
		return snapshotRows[snapshotHandoff](ctx, tx, "handoffs", local, "", func(v snapshotHandoff) error {
			key, ok := keys[v.SessionID]
			if !ok {
				return nil
			}
			_, p := feed.Encode(feed.Handoff, feed.HandoffRow{MachineID: local, Session: key, TS: v.TS, Text: v.Text}, share)
			return emit(p)
		})
	}); err != nil {
		return err
	}
	if err := array("inbox_items", func(emit func([]byte) error) error {
		return snapshotRows[model.InboxItem](ctx, tx, "inbox_items", local, " AND ts>=?", func(v model.InboxItem) error {
			key, ok := keys[v.SessionID]
			if !ok {
				return nil
			}
			_, p := feed.Encode(feed.Inbox, feed.InboxRow{InboxItem: v, Session: key}, share)
			return emit(p)
		}, now.Add(-30*24*time.Hour).UnixNano())
	}); err != nil {
		return err
	}
	if err := array("procs", func(emit func([]byte) error) error {
		return snapshotRows[model.Proc](ctx, tx, "procs", local, " AND agent<>''", func(v model.Proc) error {
			_, p := feed.Encode(feed.Procs, feed.ProcsRow{MachineID: local, Processes: []model.Proc{v}, Sessions: keys}, share)
			var bulk struct {
				Processes []json.RawMessage `json:"processes"`
			}
			if err := json.Unmarshal(p, &bulk); err != nil {
				return err
			}
			for _, row := range bulk.Processes {
				if err := emit(row); err != nil {
					return err
				}
			}
			return nil
		})
	}); err != nil {
		return err
	}
	if err := array("orphans", func(emit func([]byte) error) error {
		return snapshotRows[model.Orphan](ctx, tx, "orphans", local, "", func(v model.Orphan) error {
			_, p := feed.Encode(feed.Orphan, v, share)
			return emit(p)
		})
	}); err != nil {
		return err
	}
	if _, err := io.WriteString(w, `,"machine_sample":`); err != nil {
		return err
	}
	sample := json.RawMessage("null")
	if err := snapshotRows[model.MachineSample](ctx, tx, "machine_samples", local, " ORDER BY ts DESC LIMIT 1", func(v model.MachineSample) error {
		_, sample = feed.Encode(feed.MachineSample, v, share)
		return nil
	}); err != nil {
		return err
	}
	if err := enc.Encode(sample); err != nil {
		return err
	}
	freshness := map[string]SnapshotFresh{}
	if err := array("sources", func(emit func([]byte) error) error {
		return snapshotRows[model.SourceStatus](ctx, tx, "sources", local, "", func(v model.SourceStatus) error {
			_, p := feed.Encode(feed.Source, v, share)
			var clean model.SourceStatus
			if err := json.Unmarshal(p, &clean); err != nil {
				return err
			}
			interval := time.Minute
			if v.Name == "git" {
				interval = 5 * time.Minute
			}
			age := max(time.Duration(0), now.Sub(v.LastOKAt))
			freshness[v.Name] = SnapshotFresh{LastOKAt: clean.LastOKAt, AgeMS: age.Milliseconds(), Stale: clean.LastOKAt.IsZero() || age > 3*interval, LastError: clean.LastError}
			return emit(p)
		})
	}); err != nil {
		return err
	}
	if err := array("gaps", func(emit func([]byte) error) error {
		return snapshotRows[model.Gap](ctx, tx, "gaps", local, "", func(v model.Gap) error {
			_, p := feed.Encode(feed.Gap, v, share)
			return emit(p)
		})
	}); err != nil {
		return err
	}
	if _, err := io.WriteString(w, `},"freshness":`); err != nil {
		return err
	}
	if err := enc.Encode(freshness); err != nil {
		return err
	}
	if _, err := io.WriteString(w, `,"coverage_gaps":[`); err != nil {
		return err
	}
	first := true
	if err := snapshotRows[model.Gap](ctx, tx, "gaps", local, "", func(v model.Gap) error {
		if !first {
			if err := w.WriteByte(','); err != nil {
				return err
			}
		}
		first = false
		return enc.Encode(v)
	}); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "]}\n"); err != nil {
		return err
	}
	return w.Flush()
}
