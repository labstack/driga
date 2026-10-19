package fleet

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

// FeedRow is a decoded peer feed change.
type FeedRow = feed.FeedRow

// Batch is applied atomically by the collector, including its durable cursor.
// Status-only batches have no snapshot, rows, or cursor.
type Batch struct {
	MachineID    string
	Snapshot     *Snapshot
	Rows         []FeedRow
	Cursor       Cursor
	Peer         *Peer
	ContactAt    time.Time
	State        string
	Error        string
	Disconnected bool
	Resnapshot   bool
	ClockSkewMS  *int64
	done         chan error
}

// Complete acknowledges the writer's commit, allowing the puller to advance.
func (b Batch) Complete(err error) {
	if b.done != nil {
		b.done <- err
	}
}

// ApplyBatch resolves natural references only within the peer's machine.
// The caller must roll back its transaction if this returns an error.
func ApplyBatch(tx *store.Tx, b Batch, now time.Time) error {
	if b.MachineID == "" || b.MachineID == tx.MachineID() {
		return fmt.Errorf("refuse remote apply for local or empty machine_id")
	}
	if b.Cursor.Seq < 0 || b.Snapshot == nil && len(b.Rows) > 0 && b.Cursor.Epoch == "" {
		return store.ErrResnapshot
	}
	if b.Snapshot != nil && b.Snapshot.Schema != 2 {
		return fmt.Errorf("incompatible (schema %d)", b.Snapshot.Schema)
	}
	r := tx.ForMachine(b.MachineID)
	m, err := r.Machine()
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if b.Peer != nil {
		m.Name, m.Addr, m.DeviceID = b.Peer.Name, b.Peer.Addr, b.Peer.DeviceID
		if m.Hostname == "" {
			m.Hostname = b.Peer.Hostname
			m.OS = b.Peer.OS
		}
	}
	outageFrom := gapStart(m)
	if m.FirstSeen.IsZero() {
		m.FirstSeen = now
	}
	if b.Snapshot != nil {
		snap := b.Snapshot
		if snap.ObservedAt.IsZero() || snap.Data.Cursor.Epoch == "" || snap.Data.Cursor.Seq < 0 {
			return fmt.Errorf("invalid snapshot observation or cursor")
		}
		var machine model.Machine
		if err := json.Unmarshal(snap.Data.Machine, &machine); err != nil {
			return err
		}
		if machine.MachineID != b.MachineID {
			return fmt.Errorf("snapshot machine_id mismatch")
		}
		pending, err := tx.PendingAcks(store.Scope{Machines: []string{b.MachineID}})
		if err != nil {
			return err
		}
		if err := tx.ClearRemoteRows(b.MachineID); err != nil {
			return err
		}
		// Freeze the clock translation for this snapshot/epoch, including deletes.
		// Preserve the peer's label and all relative ages instead of deriving a
		// label from the viewer's wall clock. Future ages are clamped to zero.
		received := b.ContactAt
		if received.IsZero() {
			received = now
		}
		m.ClockSkewMS = snap.ObservedAt.Sub(received).Milliseconds()
		if b.ClockSkewMS != nil {
			m.ClockSkewMS = *b.ClockSkewMS
		}
		m.Hostname, m.OS, m.Arch, m.Cores, m.MemTotal, m.DrigaVersion = machine.Hostname, machine.OS, machine.Arch, machine.Cores, machine.MemTotal, machine.DrigaVersion
		d := &snap.Data
		groups := []struct {
			kind feed.Kind
			rows []json.RawMessage
		}{
			{feed.Repo, d.Repos}, {feed.Worktree, d.Worktrees}, {feed.Session, d.Sessions}, {feed.SessionFiles, d.SessionFiles}, {feed.Event, d.Events}, {feed.Handoff, d.Handoffs}, {feed.Summary, d.Summaries}, {feed.Inbox, d.InboxItems}, {feed.Orphan, d.Orphans}, {feed.Source, d.Sources}, {feed.Gap, d.Gaps},
		}
		for _, g := range groups {
			for _, p := range g.rows {
				if err := applyRow(r, &m, g.kind, p, now); err != nil {
					return err
				}
			}
		}
		for _, item := range pending {
			if err := r.MarkAckPending(item.ID, *item.AckPendingAt); err != nil {
				return err
			}
		}
		procs, _ := json.Marshal(struct {
			Processes []json.RawMessage `json:"processes"`
		}{d.Procs})
		if err := applyRow(r, &m, feed.Procs, procs, now); err != nil {
			return err
		}
		if len(d.MachineSample) > 0 && string(d.MachineSample) != "null" {
			if err := applyRow(r, &m, feed.MachineSample, d.MachineSample, now); err != nil {
				return err
			}
		}
		m.FeedEpoch, m.FeedSeq = d.Cursor.Epoch, d.Cursor.Seq
		m.LastSeen = minTime(snap.ObservedAt.Add(-time.Duration(m.ClockSkewMS)*time.Millisecond), now)
		if b.ContactAt.IsZero() {
			b.ContactAt = now
		}
	} else if b.Cursor.Epoch != "" {
		if m.FeedEpoch != b.Cursor.Epoch {
			return store.ErrResnapshot
		}
		initial := m.FeedSeq
		var previous int64 = -1
		for _, row := range b.Rows {
			if row.Seq <= previous {
				return fmt.Errorf("out-of-order feed sequence")
			}
			previous = row.Seq
			if row.Seq <= initial {
				continue
			}
			if row.Seq != m.FeedSeq+1 {
				return store.ErrResnapshot
			}
			if err := applyRow(r, &m, feed.Kind(row.Kind), row.Payload, now); err != nil {
				return err
			}
			m.FeedSeq = row.Seq
			if !row.TS.IsZero() {
				m.LastSeen = minTime(row.TS.Add(-time.Duration(m.ClockSkewMS)*time.Millisecond), now)
			}
		}
		if b.Cursor.Seq != m.FeedSeq && (m.FeedSeq > initial || b.Cursor.Seq > initial) {
			return store.ErrResnapshot
		}
	}
	if b.State != "" {
		m.State = b.State
	}
	if b.Disconnected {
		m.LastError = b.Error
		if !m.LastContactAt.IsZero() {
			if err := r.ExtendRemoteGap(gapStart(m), now); err != nil {
				return err
			}
		}
	} else if b.Error != "" {
		m.LastError = b.Error
	}
	if !b.ContactAt.IsZero() {
		if m.LastError != "" && !m.LastContactAt.IsZero() {
			if err := r.ExtendRemoteGap(outageFrom, b.ContactAt); err != nil {
				return err
			}
		}
		m.LastContactAt = minTime(b.ContactAt, now)
		if m.LastSeen.IsZero() {
			m.LastSeen = m.LastContactAt
		}
		m.LastError = ""
		m.State = "live"
	}
	if m.State == "" {
		m.State = "stale"
	}
	if m.State == "live" && now.Sub(m.LastContactAt) >= 45*time.Second {
		m.State = "stale"
	}
	return r.UpsertMachine(m)
}

// RefreshStates enforces the event/ping deadline even for a silent open socket.
func RefreshStates(tx *store.Tx, now time.Time) error {
	machines, err := tx.Machines()
	if err != nil {
		return err
	}
	for _, m := range machines {
		if m.MachineID == tx.MachineID() || m.State != "live" || now.Sub(m.LastContactAt) < 45*time.Second {
			continue
		}
		r := tx.ForMachine(m.MachineID)
		m.State = "stale"
		if m.LastError == "" {
			m.LastError = "no event or ping for 45s"
		}
		if err := r.ExtendRemoteGap(gapStart(m), now); err != nil {
			return err
		}
		if err := r.UpsertMachine(m); err != nil {
			return err
		}
	}
	return nil
}
func minTime(a, b time.Time) time.Time {
	if a.After(b) {
		return b
	}
	return a
}

// translateTimes maps peer times into the viewer's clock, preserving intervals.
func translateTimes(v reflect.Value, offset time.Duration, now time.Time, clamp bool) {
	if v.Kind() == reflect.Pointer {
		if !v.IsNil() {
			translateTimes(v.Elem(), offset, now, clamp)
		}
		return
	}
	if v.Type() == reflect.TypeFor[time.Time]() {
		at := v.Interface().(time.Time)
		if !at.IsZero() {
			at = at.Add(-offset)
			if clamp {
				at = minTime(at, now)
			}
			v.Set(reflect.ValueOf(at))
		}
		return
	}
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Field(i).CanSet() {
				translateTimes(v.Field(i), offset, now, clamp)
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			translateTimes(v.Index(i), offset, now, clamp)
		}
	}
}
func sessionID(r *store.MachineTx, key feed.SessionKey) (int64, error) {
	if key.Adapter == "" || key.ExternalID == "" {
		return 0, fmt.Errorf("missing natural session key")
	}
	s, err := r.SessionByKey(key.Adapter, key.ExternalID)
	if err == nil {
		return s.ID, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return 0, err
	}
	return r.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: key.Adapter, ExternalID: key.ExternalID}, Label: "unknown"})
}
func repoID(r *store.MachineTx, path string) (int64, error) {
	if path == "" {
		return 0, fmt.Errorf("missing repository path")
	}
	rs, err := r.Repos()
	if err != nil {
		return 0, err
	}
	for _, v := range rs {
		if v.Path == path {
			return v.ID, nil
		}
	}
	return r.UpsertRepo(model.Repo{Path: path})
}
func worktreeID(r *store.MachineTx, path, repo string) (int64, error) {
	ws, err := r.Worktrees(0)
	if err != nil {
		return 0, err
	}
	for _, w := range ws {
		if w.Path == path {
			return w.ID, nil
		}
	}
	id, err := repoID(r, repo)
	if err != nil {
		return 0, err
	}
	return r.UpsertWorktree(model.Worktree{Path: path, RepoID: id})
}

func applyRow(r *store.MachineTx, m *model.Machine, kind feed.Kind, payload []byte, now time.Time) error {
	row, err := feed.Decode(kind, payload)
	if err != nil {
		return err
	}
	offset := time.Duration(m.ClockSkewMS) * time.Millisecond
	keyed := kind == feed.Event || kind == feed.Handoff || kind == feed.MachineSample || kind == feed.Gap
	translateTimes(reflect.ValueOf(row), offset, now, !keyed)
	switch v := row.(type) {
	case *feed.SummaryRow:
		v.ProjectID, err = repoID(r, v.RepoPath)
		if err == nil {
			err = r.UpsertSummary(v.Summary)
		}
	case *model.Machine:
		m.Hostname, m.OS, m.Arch, m.Cores, m.MemTotal, m.DrigaVersion = v.Hostname, v.OS, v.Arch, v.Cores, v.MemTotal, v.DrigaVersion
	case *feed.SessionRow:
		if v.Adapter == "" || v.ExternalID == "" {
			return fmt.Errorf("missing session identity")
		}
		v.ID = 0
		v.ParentID = nil
		v.RepoID = nil
		v.WorktreeID = nil
		if v.Parent != nil {
			id, e := sessionID(r, *v.Parent)
			if e != nil {
				return e
			}
			v.ParentID = &id
		}
		if v.RepoPath != "" {
			id, e := repoID(r, v.RepoPath)
			if e != nil {
				return e
			}
			v.RepoID = &id
		}
		if v.WorktreePath != "" {
			id, e := worktreeID(r, v.WorktreePath, v.RepoPath)
			if e != nil {
				return e
			}
			v.WorktreeID = &id
		}
		_, err = r.UpsertSession(v.Session)
	case *feed.FilesRow:
		id, e := sessionID(r, v.Session)
		if e != nil {
			return e
		}
		err = r.ReplaceSessionFiles(id, v.Files)
	case *model.Event:
		err = r.ApplyEvent(*v)
	case *feed.HandoffRow:
		id, e := sessionID(r, v.Session)
		if e != nil {
			return e
		}
		err = r.ApplyHandoff(id, v.TS, v.Text)
	case *feed.InboxRow:
		id, e := sessionID(r, v.Session)
		if e != nil {
			return e
		}
		v.SessionID = id
		v.AckPendingAt = nil
		err = r.ApplyInbox(v.InboxItem)
	case *model.Repo:
		if v.Path == "" {
			return fmt.Errorf("missing repository path")
		}
		v.RemoteURL = feed.StripURLCredentials(v.RemoteURL)
		_, err = r.UpsertRepo(*v)
	case *feed.WorktreeRow:
		if v.Path == "" {
			return fmt.Errorf("missing worktree path")
		}
		v.RepoID, err = repoID(r, v.RepoPath)
		if err == nil {
			_, err = r.UpsertWorktree(v.Worktree)
		}
	case *model.Orphan:
		rows, e := r.Orphans()
		if e != nil {
			return e
		}
		found := false
		for i, o := range rows {
			if o.ID == v.ID {
				rows[i] = *v
				found = true
			}
		}
		if !found {
			rows = append(rows, *v)
		}
		err = r.ReplaceOrphans(rows)
	case *feed.DecodedProcs:
		ps := make([]model.Proc, 0, len(v.Processes))
		for _, p := range v.Processes {
			if p.Agent == "" {
				continue
			}
			p.SessionID = 0
			if p.Session != nil {
				p.SessionID, err = sessionID(r, *p.Session)
				if err != nil {
					return err
				}
			}
			ps = append(ps, p.Proc)
		}
		err = r.ReplaceProcs(ps)
	case *model.MachineSample:
		err = r.InsertSample(*v)
	case *model.SourceStatus:
		err = r.ApplySource(*v)
	case *model.Gap:
		err = r.InsertGap(*v)
	case *feed.DeleteRow:
		// Timestamp-bearing natural keys use the same frozen clock translation.
		key := v.Key
		convert := func(s string) (string, error) {
			at, e := time.Parse(time.RFC3339Nano, s)
			if e != nil {
				return "", e
			}
			return at.Add(-offset).UTC().Format(time.RFC3339Nano), nil
		}
		if v.Kind == feed.MachineSample {
			key, err = convert(key)
		} else if v.Kind == feed.Event || v.Kind == feed.Handoff || v.Kind == feed.Gap {
			var parts []string
			if err = json.Unmarshal([]byte(key), &parts); err == nil {
				indexes := []int{2}
				if v.Kind == feed.Gap {
					indexes = []int{0, 1}
				}
				for _, i := range indexes {
					if i >= len(parts) {
						return fmt.Errorf("invalid delete key")
					}
					parts[i], err = convert(parts[i])
					if err != nil {
						return err
					}
				}
				b, e := json.Marshal(parts)
				key, err = string(b), e
			}
		}
		if err == nil {
			err = r.DeleteRemote(v.Kind, key)
		}
	}
	return err
}

func gapStart(m model.Machine) time.Time {
	if !m.LastSeen.IsZero() {
		return m.LastSeen
	}
	return m.LastContactAt
}
