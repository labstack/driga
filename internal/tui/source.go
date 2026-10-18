// Package tui provides the interactive, read-only driga viewer.
package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/labstack/driga/internal/adapter"
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

// Snapshot holds the shared views and the evidence needed by detail panes.
type Snapshot struct {
	view.Envelope
	Hostname       string
	Summary        view.SummaryView
	Agents         view.AgentsView
	Projects       view.ProjectsView
	Inbox          view.InboxView
	Orphans        view.OrphansView
	Machine        view.MachineView
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
	Now            func() time.Time
}

// NewStoreSource wraps an already opened read-only store; its owner closes it.
func NewStoreSource(st *store.Store, dataDir string, f view.Filter) *StoreSource {
	return &StoreSource{st: st, dataDir: dataDir, Filter: f, Now: time.Now}
}

// DataVersion checks the existing connection, so tokens are comparable.
func (s *StoreSource) DataVersion(ctx context.Context) (int64, error) { return s.st.DataVersion(ctx) }

// Load builds all views inside one read transaction.
func (s *StoreSource) Load(ctx context.Context) (Snapshot, error) { return s.load(ctx, "daemon") }
func (s *StoreSource) load(ctx context.Context, mode string) (Snapshot, error) {
	snap := Snapshot{AgentDetails: map[int64]view.AgentView{}, ProjectDetails: map[int64]view.WhereView{}}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	err := s.st.Tx(ctx, func(tx *store.Tx) error {
		f := s.Filter
		var err error
		if f.Project != "" && s.ResolveProject != nil {
			f.Project, err = s.ResolveProject(tx, f.Project)
			if err != nil {
				return err
			}
		}
		if snap.Envelope, err = view.NewEnvelope(tx, mode, now, collector.Cadence(0), nil); err != nil {
			return err
		}
		if snap.Summary, err = view.Summary(tx, f, now); err != nil {
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
		if snap.Conflicts, err = view.Conflicts(tx, f, now); err != nil {
			return err
		}
		if snap.Procs, err = tx.Procs(); err != nil {
			return err
		}
		machine, err := tx.Machine()
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		snap.Hostname = machine.Hostname
		ids := map[int64]bool{}
		var collect func([]view.AgentRow)
		collect = func(rows []view.AgentRow) {
			for _, r := range rows {
				ids[r.ID] = true
				collect(r.Children)
			}
		}
		collect(snap.Agents.Rows)
		for _, i := range snap.Inbox.Rows {
			ids[i.SessionID] = true
		}
		for id := range ids {
			d, err := view.Agent(tx, id, now)
			if err != nil {
				return err
			}
			snap.AgentDetails[id] = d
		}
		for _, r := range snap.Projects.Rows {
			d, err := view.Where(tx, r.Status.Repo.Path, now)
			if err != nil {
				return err
			}
			snap.ProjectDetails[r.Status.Repo.ID] = d
		}
		if !f.Since.IsZero() {
			snap.CoverageGaps, err = tx.Gaps(f.Since)
		}
		return err
	})
	return snap, err
}

// LiveSnapshotSource collects into disposable memory while no daemon is available.
// Its mutex serializes passes, reads, and pass-counter changes.
type LiveSnapshotSource struct {
	*StoreSource
	deps    collector.Deps
	mu      sync.Mutex
	version int64
	lastErr error
	ready   chan struct{}
	done    chan struct{}
	cancel  context.CancelFunc
}

// NewLiveSnapshotSource creates a private memory database, never the daemon DB.
func NewLiveSnapshotSource(d collector.Deps, dataDir string, f view.Filter) (*LiveSnapshotSource, error) {
	var err error
	d, err = scopeDeps(d, f.Project)
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

// Limit git discovery to the requested project before Pass sees observations.
// Adapters still own their incremental log readers; machine health and process
// ancestry need a complete process snapshot even for a scoped viewer.
func scopeDeps(d collector.Deps, project string) (collector.Deps, error) {
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

// Start begins a full pass followed by five-second passes until cancellation.
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
			s.mu.Lock()
			_, s.lastErr = collector.Pass(ctx, s.deps, s.st, p)
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
			s.mu.Unlock()
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
	snap, err := s.load(ctx, "snapshot")
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
