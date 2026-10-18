package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

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

// UpsertMachine stores host information under the store's implicit machine ID.
func (tx *Tx) UpsertMachine(m model.Machine) error {
	return tx.exec(`INSERT INTO machines (id,machine_id,hostname,os,arch,cores,mem_total,first_seen,last_seen)
	VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET hostname=excluded.hostname,
	os=excluded.os,arch=excluded.arch,cores=excluded.cores,mem_total=excluded.mem_total,
	first_seen=excluded.first_seen,last_seen=excluded.last_seen`, tx.machineID, tx.machineID, m.Hostname, m.OS, m.Arch, m.Cores, m.MemTotal, unix(m.FirstSeen), unix(m.LastSeen))
}

// Machine returns the stored host information, or ErrNotFound.
func (q *querier) Machine() (model.Machine, error) {
	var m model.Machine
	var first, last int64
	err := q.conn.QueryRowContext(q.ctx, "SELECT id,hostname,os,arch,cores,mem_total,first_seen,last_seen FROM machines WHERE machine_id=?", q.machineID).Scan(&m.ID, &m.Hostname, &m.OS, &m.Arch, &m.Cores, &m.MemTotal, &first, &last)
	m.FirstSeen, m.LastSeen = fromUnix(first), fromUnix(last)
	return m, err
}

// ReplaceProcs replaces this machine's entire latest process snapshot.
func (tx *Tx) ReplaceProcs(procs []model.Proc) error {
	if err := tx.exec("DELETE FROM procs WHERE machine_id=?", tx.machineID); err != nil {
		return err
	}
	for _, p := range procs {
		if err := tx.exec(`INSERT INTO procs (machine_id,pid,ppid,start_time,rss,cpu_seconds,cpu_percent,cmdline,exe,role,agent,session_id,cwd)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`, tx.machineID, p.PID, p.PPID, unix(p.StartTime), p.RSS, p.CPUSeconds, p.CPUPercent, p.Cmdline, p.Exe, p.Role, p.Agent, p.SessionID, p.Cwd); err != nil {
			return err
		}
	}
	return nil
}

// Procs returns the latest processes in PID order.
func (q *querier) Procs() ([]model.Proc, error) {
	return readMany(q, "SELECT pid,ppid,start_time,rss,cpu_seconds,cpu_percent,cmdline,exe,role,agent,session_id,cwd FROM procs WHERE machine_id=? ORDER BY pid", []any{q.machineID}, func(s scanner) (model.Proc, error) {
		var p model.Proc
		var start int64
		err := s.Scan(&p.PID, &p.PPID, &start, &p.RSS, &p.CPUSeconds, &p.CPUPercent, &p.Cmdline, &p.Exe, &p.Role, &p.Agent, &p.SessionID, &p.Cwd)
		p.StartTime = fromUnix(start)
		return p, err
	})
}

const sampleColumns = "ts,mem_used,mem_compressed,mem_total,swap_used,swap_total,pressure_free,load1,load5,load15,disk_free,cores"

// InsertSample records a machine-health reading, replacing the same timestamp.
func (tx *Tx) InsertSample(s model.MachineSample) error {
	return tx.exec(`INSERT INTO machine_samples (machine_id,`+sampleColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(machine_id,ts) DO UPDATE SET mem_used=excluded.mem_used,mem_compressed=excluded.mem_compressed,
	mem_total=excluded.mem_total,swap_used=excluded.swap_used,swap_total=excluded.swap_total,pressure_free=excluded.pressure_free,
	load1=excluded.load1,load5=excluded.load5,load15=excluded.load15,disk_free=excluded.disk_free,cores=excluded.cores`, tx.machineID, unix(s.TS), s.MemUsed, s.MemCompressed, s.MemTotal, s.SwapUsed, s.SwapTotal, s.PressureFree, s.Load1, s.Load5, s.Load15, s.DiskFree, s.Cores)
}

func scanSample(s scanner) (model.MachineSample, error) {
	var m model.MachineSample
	var ts int64
	err := s.Scan(&ts, &m.MemUsed, &m.MemCompressed, &m.MemTotal, &m.SwapUsed, &m.SwapTotal, &m.PressureFree, &m.Load1, &m.Load5, &m.Load15, &m.DiskFree, &m.Cores)
	m.TS = fromUnix(ts)
	return m, err
}

// LatestSample returns the newest machine sample, or ErrNotFound.
func (q *querier) LatestSample() (model.MachineSample, error) {
	return scanSample(q.conn.QueryRowContext(q.ctx, "SELECT "+sampleColumns+" FROM machine_samples WHERE machine_id=? ORDER BY ts DESC LIMIT 1", q.machineID))
}

// Samples returns readings at or after since, oldest first.
func (q *querier) Samples(since time.Time) ([]model.MachineSample, error) {
	return readMany(q, "SELECT "+sampleColumns+" FROM machine_samples WHERE machine_id=? AND ts>=? ORDER BY ts", []any{q.machineID, unix(since)}, scanSample)
}

// UpsertRepo creates or updates a repo identified by path.
func (tx *Tx) UpsertRepo(r model.Repo) (int64, error) {
	return tx.upsertID("repos", "machine_id,path,name,remote_url", "path", tx.machineID, r.Path, r.Name, r.RemoteURL)
}

// Repos returns repositories ordered by path.
func (q *querier) Repos() ([]model.Repo, error) {
	return readMany(q, "SELECT id,path,name,remote_url FROM repos WHERE machine_id=? ORDER BY path", []any{q.machineID}, func(s scanner) (model.Repo, error) {
		var r model.Repo
		err := s.Scan(&r.ID, &r.Path, &r.Name, &r.RemoteURL)
		return r, err
	})
}

const worktreeColumns = "repo_id,path,branch,head,ahead,behind,dirty,untracked,oldest_dirty_mtime,last_commit_at,last_commit_subject,updated_at"

// UpsertWorktree creates or updates a checkout identified by path.
func (tx *Tx) UpsertWorktree(w model.Worktree) (int64, error) {
	return tx.upsertID("worktrees", "machine_id,"+worktreeColumns, "path", tx.machineID, w.RepoID, w.Path, w.Branch, w.Head, w.Ahead, w.Behind, w.Dirty, w.Untracked, unix(w.OldestDirtyMtime), unix(w.LastCommitAt), w.LastCommitSubject, unix(w.UpdatedAt))
}

// Worktrees returns a repo's checkouts ordered by path; repoID zero selects all.
func (q *querier) Worktrees(repoID int64) ([]model.Worktree, error) {
	return readMany(q, "SELECT id,"+worktreeColumns+" FROM worktrees WHERE machine_id=? AND (?=0 OR repo_id=?) ORDER BY path", []any{q.machineID, repoID, repoID}, func(s scanner) (model.Worktree, error) {
		var w model.Worktree
		var dirty, commit, updated int64
		err := s.Scan(&w.ID, &w.RepoID, &w.Path, &w.Branch, &w.Head, &w.Ahead, &w.Behind, &w.Dirty, &w.Untracked, &dirty, &commit, &w.LastCommitSubject, &updated)
		w.OldestDirtyMtime, w.LastCommitAt, w.UpdatedAt = fromUnix(dirty), fromUnix(commit), fromUnix(updated)
		return w, err
	})
}

const sessionColumns = "machine_id,adapter,external_id,parent_session_id,origin,version,pid,proc_start,cwd,branch,repo_id,worktree_id,title,last_prompt,registry_status,state,state_heuristic,evidence,started_at,last_activity_at,last_prompt_at,hosted,turn_open,ended_at,source_path"

// UpsertSession stores every scalar observation and derived session field.
// Completions and FilesTouched are persisted separately by the collector.
func (tx *Tx) UpsertSession(s model.Session) (int64, error) {
	return tx.upsertID("sessions", sessionColumns, "machine_id,adapter,external_id", tx.machineID, s.Adapter, s.ExternalID, s.ParentID, s.Origin, s.Version, s.PID, unix(s.ProcStart), s.Cwd, s.Branch, s.RepoID, s.WorktreeID, s.Title, s.LastPrompt, s.RegistryStatus, s.Label, s.Heuristic, s.Evidence, unix(s.StartedAt), unix(s.LastActivityAt), unix(s.LastPromptAt), s.Hosted, s.TurnOpen, nullableTime(s.EndedAt), s.SourcePath)
}

func scanSession(s scanner) (model.Session, error) {
	var m model.Session
	var machine string
	var start, procStart, activity, promptAt int64
	var ended sql.NullInt64
	err := s.Scan(&m.ID, &machine, &m.Adapter, &m.ExternalID, &m.ParentID, &m.Origin, &m.Version, &m.PID, &procStart, &m.Cwd, &m.Branch, &m.RepoID, &m.WorktreeID, &m.Title, &m.LastPrompt, &m.RegistryStatus, &m.Label, &m.Heuristic, &m.Evidence, &start, &activity, &promptAt, &m.Hosted, &m.TurnOpen, &ended, &m.SourcePath)
	m.ProcStart, m.StartedAt, m.LastActivityAt, m.LastPromptAt, m.EndedAt = fromUnix(procStart), fromUnix(start), fromUnix(activity), fromUnix(promptAt), timePointer(ended)
	return m, err
}

// Session returns a session by internal ID, or ErrNotFound.
func (q *querier) Session(id int64) (model.Session, error) {
	return scanSession(q.conn.QueryRowContext(q.ctx, "SELECT id,"+sessionColumns+" FROM sessions WHERE machine_id=? AND id=?", q.machineID, id))
}

// Sessions returns sessions matching filter, newest activity first.
func (q *querier) Sessions(filter SessionFilter) ([]model.Session, error) {
	query := "SELECT id," + sessionColumns + " FROM sessions WHERE machine_id=?"
	args := []any{q.machineID}
	if !filter.IncludeEnded {
		if filter.EndedSince.IsZero() {
			query += " AND ended_at IS NULL"
		} else {
			query += " AND (ended_at IS NULL OR ended_at>=?)"
			args = append(args, unix(filter.EndedSince))
		}
	}
	if filter.RepoPath != "" {
		query += " AND repo_id IN (SELECT id FROM repos WHERE machine_id=? AND path=?)"
		args = append(args, q.machineID, filter.RepoPath)
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
	rows, err := q.conn.QueryContext(q.ctx, "SELECT session_id,path,last_edit_at FROM session_files WHERE machine_id=? AND last_edit_at>=? ORDER BY session_id,path", q.machineID, unix(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[int64][]model.FileTouch)
	for rows.Next() {
		var id, ts int64
		var f model.FileTouch
		if err := rows.Scan(&id, &f.Path, &ts); err != nil {
			return nil, err
		}
		f.TS = fromUnix(ts)
		result[id] = append(result[id], f)
	}
	return result, rows.Err()
}

// UpsertInboxItem inserts an item once. Duplicates never change its text or ack.
func (tx *Tx) UpsertInboxItem(i model.InboxItem) error {
	return tx.exec("INSERT OR IGNORE INTO inbox_items (machine_id,id,session_id,kind,ts,summary,acked_at) VALUES (?,?,?,?,?,?,?)", tx.machineID, i.ID, i.SessionID, i.Kind, unix(i.TS), i.Summary, nullableTime(i.AckedAt))
}

// AckInbox acknowledges an existing unacknowledged item, returning whether it changed.
func (tx *Tx) AckInbox(id string, at time.Time) (bool, error) {
	r, err := tx.conn.ExecContext(tx.ctx, "UPDATE inbox_items SET acked_at=? WHERE machine_id=? AND id=? AND acked_at IS NULL", unix(at), tx.machineID, id)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

// Inbox returns items at or after since, newest first, optionally including acks.
func (q *querier) Inbox(since time.Time, includeAcked bool) ([]model.InboxItem, error) {
	return readMany(q, "SELECT id,session_id,kind,ts,summary,acked_at FROM inbox_items WHERE machine_id=? AND ts>=? AND (? OR acked_at IS NULL) ORDER BY ts DESC,id", []any{q.machineID, unix(since), includeAcked}, func(s scanner) (model.InboxItem, error) {
		var i model.InboxItem
		var ts int64
		var ack sql.NullInt64
		err := s.Scan(&i.ID, &i.SessionID, &i.Kind, &ts, &i.Summary, &ack)
		i.TS, i.AckedAt = fromUnix(ts), timePointer(ack)
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
	return readMany(q, "SELECT id,root_pid,start_time,signature,proc_count,rss_total,cwd,young,verified,evidence,pids,first_seen FROM orphans WHERE machine_id=? ORDER BY id", []any{q.machineID}, func(s scanner) (model.Orphan, error) {
		var o model.Orphan
		var start, first int64
		var evidence, pids string
		if err := s.Scan(&o.ID, &o.RootPID, &start, &o.Signature, &o.ProcCount, &o.RSSTotal, &o.Cwd, &o.Young, &o.Verified, &evidence, &pids, &first); err != nil {
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

// SetSourceError records a source's last error without changing its last success.
func (tx *Tx) SetSourceError(name, message string, at time.Time) error {
	return tx.exec(`INSERT INTO sources (machine_id,name,last_error,last_error_at) VALUES (?,?,?,?)
	ON CONFLICT(machine_id,name) DO UPDATE SET last_error=excluded.last_error,last_error_at=excluded.last_error_at`, tx.machineID, name, message, unix(at))
}

// Sources returns collection source freshness, ordered by name.
func (q *querier) Sources() ([]model.SourceStatus, error) {
	return readMany(q, "SELECT name,last_ok_at,last_error,last_error_at FROM sources WHERE machine_id=? ORDER BY name", []any{q.machineID}, func(s scanner) (model.SourceStatus, error) {
		var m model.SourceStatus
		var ok, failed int64
		err := s.Scan(&m.Name, &ok, &m.LastError, &failed)
		m.LastOKAt, m.LastErrorAt = fromUnix(ok), fromUnix(failed)
		return m, err
	})
}

// InsertGap records a collector downtime interval once.
func (tx *Tx) InsertGap(g model.Gap) error {
	return tx.exec("INSERT OR IGNORE INTO gaps (machine_id,from_ts,to_ts) VALUES (?,?,?)", tx.machineID, unix(g.From), unix(g.To))
}

// Gaps returns intervals ending at or after since, oldest first.
func (q *querier) Gaps(since time.Time) ([]model.Gap, error) {
	return readMany(q, "SELECT from_ts,to_ts FROM gaps WHERE machine_id=? AND to_ts>=? ORDER BY from_ts,to_ts", []any{q.machineID, unix(since)}, func(s scanner) (model.Gap, error) {
		var from, to int64
		err := s.Scan(&from, &to)
		return model.Gap{From: fromUnix(from), To: fromUnix(to)}, err
	})
}

// GetOffset returns a saved tail offset and whether it exists.
func (q *querier) GetOffset(path string) (model.Offset, bool, error) {
	var o model.Offset
	var mtime int64
	err := q.conn.QueryRowContext(q.ctx, "SELECT inode,size,offset,mtime FROM offsets WHERE machine_id=? AND path=?", q.machineID, path).Scan(&o.Inode, &o.Size, &o.Pos, &mtime)
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
	if key == "machine_id" && value != q.machineID {
		return fmt.Errorf("machine_id is immutable")
	}
	return q.exec("INSERT INTO meta (machine_id,key,value) VALUES (?,?,?) ON CONFLICT(key) DO UPDATE SET machine_id=excluded.machine_id,value=excluded.value", q.machineID, key, value)
}

// InsertEvent records an event and keeps the newest 200 for that adapter/session.
func (tx *Tx) InsertEvent(e model.Event) error {
	if err := tx.exec("INSERT INTO events (machine_id,adapter,external_id,ts,kind,summary) VALUES (?,?,?,?,?,?)", tx.machineID, e.Adapter, e.ExternalID, unix(e.TS), e.Kind, e.Summary); err != nil {
		return err
	}
	return tx.exec(`DELETE FROM events WHERE machine_id=? AND adapter=? AND external_id=? AND id NOT IN
	(SELECT id FROM events WHERE machine_id=? AND adapter=? AND external_id=? ORDER BY ts DESC,id DESC LIMIT 200)`, tx.machineID, e.Adapter, e.ExternalID, tx.machineID, e.Adapter, e.ExternalID)
}

func scanEvent(s scanner) (model.Event, error) {
	var e model.Event
	var ts int64
	err := s.Scan(&e.Adapter, &e.ExternalID, &ts, &e.Kind, &e.Summary)
	e.TS = fromUnix(ts)
	return e, err
}

// Events returns a session's events at or after since, oldest first.
func (q *querier) Events(adapter, externalID string, since time.Time) ([]model.Event, error) {
	return readMany(q, "SELECT adapter,external_id,ts,kind,summary FROM events WHERE machine_id=? AND adapter=? AND external_id=? AND ts>=? ORDER BY ts,id", []any{q.machineID, adapter, externalID, unix(since)}, scanEvent)
}

// LatestEvents returns events across sessions at or after since, newest first.
func (q *querier) LatestEvents(since time.Time) ([]model.Event, error) {
	return readMany(q, "SELECT adapter,external_id,ts,kind,summary FROM events WHERE machine_id=? AND ts>=? ORDER BY ts DESC,id DESC", []any{q.machineID, unix(since)}, scanEvent)
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
	return readMany(q, `SELECT h.id,h.ts,h.text FROM handoffs h JOIN sessions s ON s.id=h.session_id AND s.machine_id=h.machine_id
	WHERE h.machine_id=? AND s.repo_id=? ORDER BY h.ts DESC,h.id DESC LIMIT ?`, []any{q.machineID, repoID, limit}, func(s scanner) (model.CompletionEvent, error) {
		var e model.CompletionEvent
		var id, ts int64
		err := s.Scan(&id, &ts, &e.Text)
		e.ID, e.TS = strconv.FormatInt(id, 10), fromUnix(ts)
		return e, err
	})
}

// Prune removes expired history and keeps the latest sample in each five-minute
// bucket between 24 hours and seven days old. Samples in the last day stay intact.
func (tx *Tx) Prune(now time.Time) error {
	month, week, day := unix(now.Add(-30*24*time.Hour)), unix(now.Add(-7*24*time.Hour)), unix(now.Add(-24*time.Hour))
	for _, stmt := range []struct {
		query string
		args  []any
	}{
		{"DELETE FROM events WHERE machine_id=? AND ts<?", []any{tx.machineID, month}},
		{"DELETE FROM inbox_items WHERE machine_id=? AND ts<?", []any{tx.machineID, month}},
		{"DELETE FROM sessions WHERE machine_id=? AND ended_at<?", []any{tx.machineID, month}},
		{"DELETE FROM machine_samples WHERE machine_id=? AND ts<?", []any{tx.machineID, week}},
		{`DELETE FROM machine_samples WHERE machine_id=? AND ts<? AND ts NOT IN
		(SELECT MAX(ts) FROM machine_samples WHERE machine_id=? AND ts>=? AND ts<? GROUP BY ts/?)`, []any{tx.machineID, day, tx.machineID, week, day, int64(5 * time.Minute)}},
		{"DELETE FROM gaps WHERE machine_id=? AND to_ts<?", []any{tx.machineID, month}},
	} {
		if err := tx.exec(stmt.query, stmt.args...); err != nil {
			return err
		}
	}
	return nil
}

// Offsets loads tail positions once before concurrent source collection.
func (q *querier) Offsets() (map[string]model.Offset, error) {
	rows, err := q.conn.QueryContext(q.ctx, "SELECT path,inode,size,offset,mtime FROM offsets WHERE machine_id=?", q.machineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]model.Offset)
	for rows.Next() {
		var path string
		var o model.Offset
		var mtime int64
		if err := rows.Scan(&path, &o.Inode, &o.Size, &o.Pos, &mtime); err != nil {
			return nil, err
		}
		o.Mtime = fromUnix(mtime)
		result[path] = o
	}
	return result, rows.Err()
}
