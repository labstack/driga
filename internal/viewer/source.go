// Package viewer supplies shared presentation snapshots for terminal and browser viewers.
package viewer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/gitscan"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

// Source supplies consistent presentation snapshots and a cheap change token.
type Source interface {
	Load(context.Context) (Snapshot, error)
	DataVersion(context.Context) (int64, error)
}

// ScopedSource also supports per-request browser machine selection.
type ScopedSource interface {
	Source
	LoadScope(context.Context, store.Scope, ...bool) (Snapshot, error)
}

// Snapshot holds the shared views and the evidence needed by detail panes.
type Snapshot struct {
	BoardData view.BoardSnapshot
	Tail      []view.TailEvent
	Cleanup   []cleanup.Candidate
	view.Envelope
	LocalMachineID string
	Machines       view.MachinesView
	Hostname       string
	Summary        view.SummaryView
	Agents         view.AgentsView
	Projects       view.ProjectsView
	Inbox          view.InboxView
	Orphans        view.OrphansView
	Machine        view.MachineView
	PanelHistory   view.PanelHistoryView
	SessionPanels  []view.SessionPanel
	ToolActivity   []view.ToolHour
	Activity       view.ActivityView
	Conflicts      view.ConflictsView
	AgentDetails   map[int64]view.AgentView
	ProjectDetails map[int64]view.WhereView
	Procs          []model.Proc
}

// StoreSource reads the daemon database without acquiring its writer lock.
type StoreSource struct {
	st      *store.Store
	dataDir string
	Filter  view.Filter
	// ResolveProject applies the CLI's canonical path/name resolution in the read transaction.
	ResolveProject func(view.Reader, string) (string, error)
	ResolveFilter  func(view.Reader, view.Filter) (view.Filter, error)
	Now            func() time.Time
}

// NewStoreSource wraps an already opened read-only store; its owner closes it.
func NewStoreSource(st *store.Store, dataDir string, f view.Filter) *StoreSource {
	return &StoreSource{st: st, dataDir: dataDir, Filter: f, Now: time.Now}
}

// DataVersion checks the existing connection, so tokens are comparable.
func (s *StoreSource) DataVersion(ctx context.Context) (int64, error) { return s.st.DataVersion(ctx) }

// Load builds overview views inside one read transaction; selection loads details.
func (s *StoreSource) Load(ctx context.Context) (Snapshot, error) {
	return s.load(ctx, "daemon", store.Scope{})
}

// LoadScope builds views for a browser-selected machine scope. An optional all
// overrides visibility for this request without mutating the source filter.
func (s *StoreSource) LoadScope(ctx context.Context, scope store.Scope, all ...bool) (Snapshot, error) {
	return s.load(ctx, "daemon", scope, all...)
}

func (s *StoreSource) load(ctx context.Context, mode string, scope store.Scope, all ...bool) (Snapshot, error) {
	snap := Snapshot{AgentDetails: map[int64]view.AgentView{}, ProjectDetails: map[int64]view.WhereView{}}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	err := s.st.Tx(ctx, func(tx *store.Tx) error {
		snap.LocalMachineID = tx.MachineID()
		f := s.Filter
		if len(all) > 0 {
			f.All = all[0]
		}
		var err error
		if s.ResolveFilter != nil {
			f, err = s.ResolveFilter(tx, f)
			if err != nil {
				return err
			}
		} else if f.Project != "" && s.ResolveProject != nil {
			f.Project, err = s.ResolveProject(tx, f.Project)
			if err != nil {
				return err
			}
		}
		if scope.Machines != nil {
			selected := []string{}
			for _, id := range scope.Machines {
				if f.Scope.Machines == nil {
					selected = append(selected, id)
				} else {
					for _, allowed := range f.Scope.Machines {
						if id == allowed {
							selected = append(selected, id)
						}
					}
				}
			}
			f.Scope = store.Scope{Machines: selected}
		}
		if snap.Envelope, err = view.NewEnvelope(tx, mode, now, collector.Cadence(0), nil, f.Scope); err != nil {
			return err
		}
		if snap.Machines, err = view.Machines(tx, now); err != nil {
			return err
		}
		if snap.Summary, err = view.Summary(tx, f, now); err != nil {
			return err
		}
		if snap.BoardData, err = view.LoadBoardSnapshot(tx, f, now); err != nil {
			return err
		}
		if snap.Agents, err = view.Agents(tx, f, now); err != nil {
			return err
		}
		if snap.Projects, err = view.Projects(tx, f, now); err != nil {
			return err
		}
		if snap.Inbox, err = view.Inbox(tx, f, now); err != nil {
			return err
		}
		if snap.Orphans, err = view.Orphans(tx, f, now); err != nil {
			return err
		}
		if snap.Machine, err = view.Machine(tx, f, now); err != nil {
			return err
		}
		if snap.Activity, err = view.Activity(tx, f, now); err != nil {
			return err
		}
		if snap.PanelHistory, err = view.PanelHistory(tx, f, now); err != nil {
			return err
		}
		if snap.SessionPanels, snap.ToolActivity, err = view.FleetPanels(tx, f, now); err != nil {
			return err
		}
		if snap.Tail, err = view.EventTail(tx, f.Scope, now); err != nil {
			return err
		}
		if snap.Conflicts, err = view.Conflicts(tx, f, now); err != nil {
			return err
		}
		if snap.Procs, err = tx.Procs(); err != nil {
			return err
		}
		// A collector whose machine source never succeeded has no machine row;
		// the views above still hold its sessions.
		if machine, err := tx.Machine(); err == nil {
			snap.Hostname = machine.Hostname
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if !f.Since.IsZero() {
			snap.CoverageGaps, err = view.CoverageGaps(tx, f.Since, f.Scope)
		}
		return err
	})
	return snap, err
}

// LiveSnapshotSource collects into disposable memory while no daemon is available.
// The database serializes commits and reads. Its mutex protects publication
// metadata, never slow filesystem/subprocess collection.
type LiveSnapshotSource struct {
	*StoreSource
	deps      collector.Deps
	mu        sync.Mutex
	version   int64
	lastErr   error
	ready     chan struct{}
	done      chan struct{}
	cancel    context.CancelFunc
	idleSince time.Time
}

// NewLiveSnapshotSource creates a private memory database, never the daemon DB.
func NewLiveSnapshotSource(d collector.Deps, dataDir string, f view.Filter) (*LiveSnapshotSource, error) {
	var err error
	d, err = ScopeDeps(d, f.Project)
	if err != nil {
		return nil, err
	}
	st, err := store.Open("", store.Memory)
	if err != nil {
		return nil, err
	}
	d.State = collector.NewState()
	return &LiveSnapshotSource{StoreSource: NewStoreSource(st, dataDir, f), deps: d, ready: make(chan struct{}), done: make(chan struct{})}, nil
}

// ScopeDeps limits git discovery to the requested project before Pass sees observations.
// Adapters still own their incremental log readers; machine health and process
// ancestry need a complete process snapshot even for a scoped viewer.
func ScopeDeps(d collector.Deps, project string) (collector.Deps, error) {
	if project == "" {
		return d, nil
	}
	byName := project != "." && !strings.ContainsAny(project, "/\\")
	if _, err := os.Stat(config.ExpandHome(project)); err == nil {
		byName = false
	}
	root := project
	if !byName {
		root = gitscan.NormalizePath(config.ExpandHome(project))
		repos, err := gitscan.DiscoverRepos([]string{root}, nil, 0, nil)
		if err != nil {
			return d, err
		}
		if len(repos) > 0 {
			root = repos[0]
		}
		d.Cfg.Roots = []string{root}
	} else {
		var roots []string
		for _, base := range d.Cfg.Roots {
			if filepath.Base(base) == project {
				roots = append(roots, base)
			} else {
				roots = append(roots, filepath.Join(base, project))
			}
		}
		d.Cfg.Roots = roots
	}
	d.Cfg.RootDepth = 0
	adapters := make([]adapter.Adapter, len(d.Adapters))
	for i, a := range d.Adapters {
		adapters[i] = projectAdapter{Adapter: a, root: root, byName: byName}
	}
	d.Adapters = adapters
	return d, nil
}

type projectAdapter struct {
	adapter.Adapter
	root   string
	byName bool
}

func (a projectAdapter) Collect(ctx context.Context, env adapter.Env) ([]model.SessionObs, []adapter.Warning, error) {
	obs, warnings, err := a.Adapter.Collect(ctx, env)
	if err != nil {
		return nil, warnings, err
	}
	selected := make([]model.SessionObs, 0, len(obs))
	for _, o := range obs {
		if o.Cwd == "" {
			continue
		}
		repos, err := gitscan.DiscoverRepos([]string{o.Cwd}, nil, 0, nil)
		if err != nil {
			return nil, warnings, err
		}
		for _, repo := range repos {
			if !a.byName && repo == a.root || a.byName && filepath.Base(repo) == a.root {
				selected = append(selected, o)
				break
			}
		}
	}
	return selected, warnings, nil
}

// Start publishes project identities, sessions and machine health before Git enrichment, followed
// by five-second passes until cancellation.
// Call Start exactly once, before reading, and Close after cancelling the viewer.
func (s *LiveSnapshotSource) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	go func() {
		defer close(s.done)
		intervals := collector.Cadence(0)
		var procs, machine, git, full, cwds time.Time
		first := true
		ticker := time.NewTicker(intervals.Procs)
		defer ticker.Stop()
		for {
			now := time.Now()
			due := func(last time.Time, d time.Duration) bool { return last.IsZero() || now.Sub(last) >= d }
			p := collector.Plan{Procs: due(procs, intervals.Procs), Machine: due(machine, intervals.Machine), Git: due(git, intervals.Git), GitFull: due(full, intervals.GitFull), Cwds: due(cwds, intervals.Cwds), Sockets: due(procs, intervals.Procs)}
			p.Git = p.Git || p.GitFull
			if first {
				// Repository status (especially untracked files) and socket
				// probes must not delay the first populated interactive frame.
				p.Git, p.GitFull, p.Sockets = false, false, false
				p.GitIdentity = true
			}
			_, passErr := collector.Pass(ctx, s.deps, s.st, p)
			if passErr == nil {
				passErr = s.st.Tx(ctx, func(tx *store.Tx) error { return tx.PruneSnapshot(now) })
			}
			s.mu.Lock()
			s.lastErr = passErr
			if s.lastErr == nil {
				s.version++
				if p.Procs {
					procs = now
				}
				if p.Machine {
					machine = now
				}
				if p.Git {
					git = now
				}
				if p.GitFull {
					full = now
				}
				if p.Cwds {
					cwds = now
				}
			}
			delay := s.collectionInterval(now)
			s.mu.Unlock()
			ticker.Reset(delay)
			if first {
				close(s.ready)
				first = false
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// Load waits for the first pass and returns a consistent disposable snapshot.
func (s *LiveSnapshotSource) Load(ctx context.Context) (Snapshot, error) {
	return s.loadSnapshot(ctx, store.Scope{})
}

// LoadScope serializes scoped reads with disposable snapshot collection.
func (s *LiveSnapshotSource) LoadScope(ctx context.Context, scope store.Scope, all ...bool) (Snapshot, error) {
	return s.loadSnapshot(ctx, scope, all...)
}

func (s *LiveSnapshotSource) loadSnapshot(ctx context.Context, scope store.Scope, all ...bool) (Snapshot, error) {
	select {
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	case <-s.ready:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastErr != nil {
		return Snapshot{}, s.lastErr
	}
	snap, err := s.load(ctx, "snapshot", scope, all...)
	return snap, err
}

// DataVersion is a successful-pass counter, including writes on this connection.
func (s *LiveSnapshotSource) DataVersion(ctx context.Context) (int64, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-s.ready:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.version, s.lastErr
}

// Close waits for collection to stop before releasing the memory database.
func (s *LiveSnapshotSource) Close() error {
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}
	return s.st.Close()
}

// DetailSource loads only the selected session or project detail.
type DetailSource interface {
	LoadAgent(context.Context, store.Scope, int64) (view.AgentView, error)
	LoadProject(context.Context, store.Scope, int64, bool) (view.WhereView, error)
}

func (s *StoreSource) LoadAgent(ctx context.Context, scope store.Scope, id int64) (view.AgentView, error) {
	var result view.AgentView
	err := s.st.Tx(ctx, func(tx *store.Tx) error { var err error; result, err = view.Agent(tx, id, s.Now(), scope); return err })
	return result, err
}
func (s *StoreSource) LoadProject(ctx context.Context, scope store.Scope, id int64, all bool) (view.WhereView, error) {
	var result view.WhereView
	err := s.st.Tx(ctx, func(tx *store.Tx) error {
		rows, err := view.Projects(tx, view.Filter{Scope: scope, All: all}, s.Now())
		if err != nil {
			return err
		}
		for _, r := range rows.Rows {
			if r.Status.Repo.ID == id {
				result, err = view.WhereFiltered(tx, r.Status.Repo.Path, s.Now(), view.Filter{Scope: store.Scope{Machines: []string{r.MachineID}}, All: all})
				return err
			}
		}
		return store.ErrNotFound
	})
	return result, err
}
func (s *LiveSnapshotSource) LoadAgent(ctx context.Context, scope store.Scope, id int64) (view.AgentView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.StoreSource.LoadAgent(ctx, scope, id)
}
func (s *LiveSnapshotSource) LoadProject(ctx context.Context, scope store.Scope, id int64, all bool) (view.WhereView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.StoreSource.LoadProject(ctx, scope, id, all)
}

// SetActive lets the browser back off after its last SSE client leaves. TUIs
// never set it, and keep their interactive collection cadence.
func (s *LiveSnapshotSource) SetActive(active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setActiveAt(active, time.Now())
}
func (s *LiveSnapshotSource) setActiveAt(active bool, now time.Time) {
	if active {
		s.idleSince = time.Time{}
	} else if s.idleSince.IsZero() {
		s.idleSince = now
	}
}
func (s *LiveSnapshotSource) collectionInterval(now time.Time) time.Duration {
	if !s.idleSince.IsZero() && now.Sub(s.idleSince) > time.Minute {
		return time.Minute
	}
	return 5 * time.Second
}

// FlowSource loads one family on demand without expanding every overview row.
type FlowSource interface {
	LoadFlow(context.Context, store.Scope, int64) (view.FlowView, error)
}

func (s *StoreSource) LoadFlow(ctx context.Context, scope store.Scope, id int64) (view.FlowView, error) {
	return s.LoadFlowWindow(ctx, scope, id, 60)
}

// FlowWindowSource selects the same contract at a supported zoom.
type FlowWindowSource interface {
	LoadFlowWindow(context.Context, store.Scope, int64, int) (view.FlowView, error)
}

func (s *StoreSource) LoadFlowWindow(ctx context.Context, scope store.Scope, id int64, minutes int) (view.FlowView, error) {
	var result view.FlowView
	err := s.st.Tx(ctx, func(tx *store.Tx) error {
		f := s.Filter
		var err error
		if s.ResolveFilter != nil {
			f, err = s.ResolveFilter(tx, f)
		} else if f.Project != "" && s.ResolveProject != nil {
			f.Project, err = s.ResolveProject(tx, f.Project)
		}
		if err != nil {
			return err
		}
		if scope.Machines != nil {
			allowed := []string{}
			for _, id := range scope.Machines {
				if f.Scope.Machines == nil {
					allowed = append(allowed, id)
				} else {
					for _, a := range f.Scope.Machines {
						if a == id {
							allowed = append(allowed, id)
						}
					}
				}
			}
			f.Scope = store.Scope{Machines: allowed}
		}
		f.FlowMinutes = minutes
		result, err = view.Flow(tx, f, id, s.Now())
		return err
	})
	return result, err
}
func (s *LiveSnapshotSource) LoadFlow(ctx context.Context, scope store.Scope, id int64) (view.FlowView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.StoreSource.LoadFlow(ctx, scope, id)
}

func (s *LiveSnapshotSource) LoadFlowWindow(ctx context.Context, scope store.Scope, id int64, minutes int) (view.FlowView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.StoreSource.LoadFlowWindow(ctx, scope, id, minutes)
}
