package view

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/labstack/driga/internal/inbox"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

// MachineState reports freshness without changing persisted collector state.
func MachineState(m model.Machine, local string, now time.Time) string {
	if m.MachineID == local {
		return "local"
	}
	if m.State == "" || m.State == "live" && (m.LastContactAt.IsZero() || now.Sub(m.LastContactAt) >= 45*time.Second) {
		return "stale"
	}
	return m.State
}

// MachineName supplies a stable human-readable machine label.
func MachineName(m model.Machine) string {
	if m.Name != "" {
		return m.Name
	}
	if m.Hostname != "" {
		return m.Hostname
	}
	return m.MachineID
}

func lastSeen(m model.Machine) time.Time {
	if !m.LastContactAt.IsZero() {
		return m.LastContactAt
	}
	return m.LastSeen
}
func freshness(m model.Machine, local string, now time.Time) MachineFresh {
	state := MachineState(m, local, now)
	seen := lastSeen(m)
	return MachineFresh{MachineName: MachineName(m), State: state, Stale: state != "local" && state != "live", LastSeen: seen, LastSeenAge: seenAge(now, seen)}
}
func selected(scope store.Scope, id string) bool {
	if scope.Machines == nil {
		return true
	}
	for _, v := range scope.Machines {
		if v == id {
			return true
		}
	}
	return false
}
func scopeOf(f Filter, scopes []store.Scope) store.Scope {
	if len(scopes) > 0 {
		return scopes[0]
	}
	return f.Scope
}

func machineReaders(r Reader, scope store.Scope, fn func(Reader, model.Machine) error) error {
	ms, e := r.Machines()
	if e != nil {
		return e
	}
	found := false
	for _, m := range ms {
		if m.MachineID == r.MachineID() {
			found = true
		}
	}
	if !found {
		ms = append(ms, model.Machine{MachineID: r.MachineID(), State: "local"})
	}
	// Keep the local machine first; all other rows have a deterministic order.
	sort.SliceStable(ms, func(i, j int) bool {
		if ms[i].MachineID == r.MachineID() || ms[j].MachineID == r.MachineID() {
			return ms[i].MachineID == r.MachineID() && ms[j].MachineID != r.MachineID()
		}
		return ms[i].MachineID < ms[j].MachineID
	})
	for _, m := range ms {
		if selected(scope, m.MachineID) {
			if e := fn(r.ReadMachine(m.MachineID), m); e != nil {
				return e
			}
		}
	}
	return nil
}

// Machines lists machine state, last contact, cached counts, and clock warnings.
func Machines(r Reader, now time.Time, scopes ...store.Scope) (MachinesView, error) {
	v := MachinesView{Rows: []MachineRow{}}
	scope := store.Scope{}
	if len(scopes) > 0 {
		scope = scopes[0]
	}
	e := machineReaders(r, scope, func(mr Reader, m model.Machine) error {
		ss, e := mr.Sessions(store.SessionFilter{})
		if e != nil {
			return e
		}
		items, e := mr.Inbox(time.Time{}, false)
		if e != nil {
			return e
		}
		count := 0
		for _, i := range items {
			if i.Kind != "worker_finished" {
				count++
			}
		}
		m.State = MachineState(m, r.MachineID(), now)
		row := MachineRow{Machine: m, Agents: len(visibleSessions(ss, false, now)), Inbox: count, LastSeenAge: seenAge(now, lastSeen(m))}
		if m.ClockSkewMS > 60000 || m.ClockSkewMS < -60000 {
			row.SkewWarning = fmt.Sprintf("clock skew %s (> 60s)", time.Duration(m.ClockSkewMS)*time.Millisecond)
		}
		v.Rows = append(v.Rows, row)
		return nil
	})
	return v, e
}

// Agents merges independently built machine-local families.
func Agents(r Reader, f AgentFilter, now time.Time, scopes ...store.Scope) (AgentsView, error) {
	v := AgentsView{Rows: []AgentRow{}}
	e := machineReaders(r, scopeOf(f, scopes), func(mr Reader, m model.Machine) error {
		a, e := agentsLocal(mr, f, now)
		if e != nil {
			return e
		}
		fresh := freshness(m, r.MachineID(), now)
		tagAgents(a.Rows, fresh)
		v.Rows = append(v.Rows, a.Rows...)
		return nil
	})
	return v, e
}

// Agent resolves a globally unique stored session ID within the selected scope.
func Agent(r Reader, id int64, now time.Time, scopes ...store.Scope) (AgentView, error) {
	v := AgentView{}
	found := false
	scope := store.Scope{}
	if len(scopes) > 0 {
		scope = scopes[0]
	}
	e := machineReaders(r, scope, func(mr Reader, m model.Machine) error {
		if found {
			return nil
		}
		a, e := agentLocal(mr, id, now)
		if errors.Is(e, store.ErrNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		a.MachineFresh = freshness(m, r.MachineID(), now)
		v = a
		found = true
		return nil
	})
	if e == nil && !found {
		e = store.ErrNotFound
	}
	return v, e
}

// Projects keeps identical paths on different machines separate.
func Projects(r Reader, f Filter, now time.Time, scopes ...store.Scope) (ProjectsView, error) {
	v := ProjectsView{Rows: []ProjectRow{}}
	e := machineReaders(r, scopeOf(f, scopes), func(mr Reader, m model.Machine) error {
		p, e := projectsLocal(mr, f, now)
		for i := range p.Rows {
			p.Rows[i].MachineFresh = freshness(m, r.MachineID(), now)
		}
		v.Rows = append(v.Rows, p.Rows...)
		return e
	})
	return v, e
}

// Where requires an unambiguous machine for a canonical repository path.
func Where(r Reader, path string, now time.Time, scopes ...store.Scope) (WhereView, error) {
	scope := store.Scope{}
	if len(scopes) > 0 {
		scope = scopes[0]
	}
	return WhereFiltered(r, path, now, Filter{Scope: scope})
}

// WhereFiltered applies the same session visibility as project and agent lists.
func WhereFiltered(r Reader, path string, now time.Time, f Filter) (WhereView, error) {
	v := WhereView{}
	found := false
	scope := f.Scope
	e := machineReaders(r, scope, func(mr Reader, m model.Machine) error {
		repos, e := mr.Repos()
		if e != nil {
			return e
		}
		for _, repo := range repos {
			if repo.Path == path {
				if found {
					return fmt.Errorf("project %q exists on multiple machines; use --machine", path)
				}
				v, e = whereLocal(mr, path, now, f.All)
				v.MachineFresh = freshness(m, r.MachineID(), now)
				found = true
				return e
			}
		}
		return nil
	})
	if e == nil && !found {
		e = fmt.Errorf("unknown project %q", path)
	}
	return v, e
}

// Inbox merges attention rows while retaining machine identity.
func Inbox(r Reader, f Filter, now time.Time, scopes ...store.Scope) (InboxView, error) {
	v := InboxView{Rows: []InboxRow{}}
	e := machineReaders(r, scopeOf(f, scopes), func(mr Reader, m model.Machine) error {
		a, e := inboxLocal(mr, f, now)
		for i := range a.Rows {
			a.Rows[i].MachineFresh = freshness(m, r.MachineID(), now)
		}
		v.Rows = append(v.Rows, a.Rows...)
		return e
	})
	sort.SliceStable(v.Rows, func(i, j int) bool { return inbox.Less(v.Rows[i].InboxItem, v.Rows[j].InboxItem) })
	return v, e
}

// Conflicts never compares file paths or sessions across machine boundaries.
func Conflicts(r Reader, f Filter, now time.Time, scopes ...store.Scope) (ConflictsView, error) {
	v := ConflictsView{Current: []ConflictRow{}, Recent: []ConflictRow{}}
	e := machineReaders(r, scopeOf(f, scopes), func(mr Reader, m model.Machine) error {
		a, e := conflictsLocal(mr, f, now)
		for i := range a.Current {
			a.Current[i].MachineFresh = freshness(m, r.MachineID(), now)
		}
		for i := range a.Recent {
			a.Recent[i].MachineFresh = freshness(m, r.MachineID(), now)
		}
		v.Current = append(v.Current, a.Current...)
		v.Recent = append(v.Recent, a.Recent...)
		return e
	})
	return v, e
}

// Orphans merges reclaim candidates without enabling remote process actions.
func Orphans(r Reader, f Filter, now time.Time, scopes ...store.Scope) (OrphansView, error) {
	v := OrphansView{Rows: []OrphanRow{}}
	e := machineReaders(r, scopeOf(f, scopes), func(mr Reader, m model.Machine) error {
		a, e := orphansLocal(mr, f, now)
		for i := range a.Rows {
			a.Rows[i].MachineFresh = freshness(m, r.MachineID(), now)
		}
		v.Rows = append(v.Rows, a.Rows...)
		return e
	})
	return v, e
}

// Machine includes each selected host's health and process-memory totals.
func Machine(r Reader, f Filter, now time.Time, scopes ...store.Scope) (MachineView, error) {
	v := MachineView{MachineID: r.MachineID(), Memory: []MemoryRow{}, Machines: []MachineView{}}
	e := machineReaders(r, scopeOf(f, scopes), func(mr Reader, m model.Machine) error {
		a, e := machineLocal(mr, f, now)
		if e != nil {
			return e
		}
		a.MachineFresh = freshness(m, r.MachineID(), now)
		for i := range a.Memory {
			a.Memory[i].MachineFresh = a.MachineFresh
		}
		if len(v.Machines) == 0 || m.MachineID == r.MachineID() {
			v.Sample = a.Sample
			v.TopMemory, v.TopCPU = a.TopMemory, a.TopCPU
			v.MCPServers = a.MCPServers
			v.AgentShare, v.SessionCosts = a.AgentShare, a.SessionCosts
			v.AgentNoSession, v.LikelyAgent, v.OtherApps = a.AgentNoSession, a.LikelyAgent, a.OtherApps
			v.MachineID = a.MachineID
			v.MachineFresh = a.MachineFresh
		}
		v.Machines = append(v.Machines, a)
		v.Memory = append(v.Memory, a.Memory...)
		return nil
	})
	return v, e
}

// Summary totals the selected machines and uses the local host's health sample.
func Summary(r Reader, f Filter, now time.Time, scopes ...store.Scope) (SummaryView, error) {
	v := SummaryView{MachineID: r.MachineID()}
	first := true
	e := machineReaders(r, scopeOf(f, scopes), func(mr Reader, m model.Machine) error {
		a, e := summaryLocal(mr, f, now)
		if e != nil {
			return e
		}
		v.Agents += a.Agents
		v.Working += a.Working
		v.Waiting += a.Waiting
		v.Stalled += a.Stalled
		v.Inbox += a.Inbox
		v.Orphans += a.Orphans
		v.Reclaim += a.Reclaim
		if first || m.MachineID == r.MachineID() {
			v.Sample = a.Sample
			v.MachineID = a.MachineID
		}
		first = false
		return nil
	})
	return v, e
}

// CoverageGaps merges machine-tagged coverage within a selected scope.
func CoverageGaps(r Reader, since time.Time, scope store.Scope) ([]model.Gap, error) {
	gaps := []model.Gap{}
	e := machineReaders(r, scope, func(mr Reader, _ model.Machine) error {
		rows, e := mr.Gaps(since)
		gaps = append(gaps, rows...)
		return e
	})
	return gaps, e
}

// NewEnvelope includes freshness and coverage for the same machines as its data.
func NewEnvelope(r Reader, mode string, now time.Time, intervals Intervals, data any, scopes ...store.Scope) (Envelope, error) {
	v := Envelope{Schema: 2, ObservedAt: now, Mode: mode, Freshness: map[string]Fresh{}, CoverageGaps: []model.Gap{}, Data: data}
	scope := store.Scope{}
	if len(scopes) > 0 {
		scope = scopes[0]
	}
	e := machineReaders(r, scope, func(mr Reader, m model.Machine) error {
		e, err := newEnvelopeLocal(mr, mode, now, intervals, data)
		if err != nil {
			return err
		}
		for key, f := range e.Freshness {
			if m.MachineID != r.MachineID() {
				key = m.MachineID + "/" + key
				f.Stale = f.Stale || freshness(m, r.MachineID(), now).Stale
			}
			v.Freshness[key] = f
		}
		if m.MachineID != r.MachineID() {
			f := freshness(m, r.MachineID(), now)
			v.Freshness[m.MachineID] = Fresh{LastOKAt: f.LastSeen, AgeMS: f.LastSeenAge.Milliseconds(), Stale: f.Stale, LastError: m.LastError}
		}
		v.CoverageGaps = append(v.CoverageGaps, e.CoverageGaps...)
		return nil
	})
	return v, e
}

func seenAge(now, seen time.Time) time.Duration {
	if seen.IsZero() {
		return 0
	}
	return max(0, now.Sub(seen))
}
func tagAgents(rows []AgentRow, fresh MachineFresh) {
	for i := range rows {
		rows[i].MachineFresh = fresh
		tagAgents(rows[i].Children, fresh)
	}
}
