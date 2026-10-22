package collector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/gitscan"
	attention "github.com/labstack/driga/internal/inbox"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/store"
)

// Source workers read immutable offsets; their deltas are committed in order.
type sourceOffsets struct {
	prev map[string]model.Offset
	next map[string]model.Offset
}

func (o *sourceOffsets) GetOffset(path string) (model.Offset, bool, error) {
	if v, ok := o.next[path]; ok {
		return v, true, nil
	}
	v, ok := o.prev[path]
	return v, ok, nil
}
func (o *sourceOffsets) SetOffset(path string, v model.Offset) error { o.next[path] = v; return nil }

type adapterResult struct {
	name     string
	obs      []model.SessionObs
	warnings []adapter.Warning
	err      error
	offsets  *sourceOffsets
}
type repoResult struct {
	path      string
	worktrees []model.Worktree
	err       error
}
type sourceResults struct {
	agents       []attention.Agent
	sessions     []model.Session
	original     map[int64]model.Session
	procs        []model.Proc
	procErr      error
	procAt       time.Time
	sample       model.MachineSample
	machineErr   error
	adapters     []adapterResult
	observations []model.SessionObs
	first        bool
	baseline     time.Time
	cwdRan       bool
	cwdErr       error
	git          []repoResult
	gitErr       error
}

func collectSources(ctx context.Context, d Deps, st *store.Store, p Plan, state *State, now time.Time) (sourceResults, error) {
	r := sourceResults{baseline: now}
	base, err := st.Meta("baseline_at")
	r.first = errors.Is(err, store.ErrNotFound)
	if err != nil && !r.first {
		return r, err
	}
	if !r.first {
		r.baseline, err = time.Parse(time.RFC3339Nano, base)
		if err != nil {
			return r, fmt.Errorf("baseline_at: %w", err)
		}
	}
	offsets, err := st.Offsets()
	if err != nil {
		return r, err
	}
	ss, err := st.Sessions(store.SessionFilter{IncludeEnded: true})
	if err != nil {
		return r, err
	}
	var knownRepos []model.Repo
	if p.Git {
		knownRepos, err = st.Repos()
		if err != nil {
			return r, err
		}
	}
	for _, a := range d.Adapters {
		if d.Cfg.Adapters[a.Name()].Enabled {
			r.adapters = append(r.adapters, adapterResult{name: a.Name(), offsets: &sourceOffsets{prev: offsets, next: make(map[string]model.Offset)}})
		}
	}
	// Stored rows are only a fallback; rereading every row each pass is the
	// collector's largest allocation, so read them only when no scan runs.
	var storedProcs []model.Proc
	if !p.Procs {
		if storedProcs, err = state.storedProcs(st.Procs); err != nil {
			return r, err
		}
	}
	procReady := make(chan struct{})
	var wg sync.WaitGroup
	if d.InboxRunner != nil && d.Cfg.Adapters["claude"].Enabled {
		// Optional CLI support varies by installation/release. Missing, slow or
		// malformed results are skipped without turning intake health red.
		wg.Go(func() { r.agents, _ = attention.PollAgents(ctx, d.InboxRunner, 200*time.Millisecond) })
	}
	if p.Machine {
		wg.Go(func() { r.sample, r.machineErr = d.Plat.Machine(ctx) })
	}
	var adapters sync.WaitGroup
	i := 0
	for _, a := range d.Adapters {
		if !d.Cfg.Adapters[a.Name()].Enabled {
			continue
		}
		result := &r.adapters[i]
		i++
		adapters.Go(func() {
			result.obs, result.warnings, result.err = a.Collect(ctx, adapter.Env{Home: os.Getenv("HOME"), Now: now, CaptureSnippets: d.Cfg.CaptureSnippets(), Offsets: result.offsets, Alive: d.Plat.Alive, Baseline: r.first})
		})
	}
	wg.Go(func() {
		defer close(procReady)
		// Snapshot reuses its seed and samples again only after adapters have
		// finished, measuring the actual interval without an artificial sleep.
		if d.snapshot {
			adapters.Wait()
		}
		if p.Procs {
			r.procAt = now
			if d.snapshot {
				r.procAt = time.Now()
			}
			r.procs, r.procErr = d.Plat.Procs(ctx)
		} else {
			r.procs = storedProcs
		}
		if r.procErr != nil {
			return
		}
		for i := range r.procs {
			if d.Sigs != nil && p.Procs {
				sig, _ := d.Sigs.Match(r.procs[i].Cmdline, r.procs[i].Exe)
				r.procs[i].Role, r.procs[i].Agent = string(sig.Role), sig.Agent
			}
		}
		state.applyCwds(r.procs)
		refresh := p.Cwds && (state.cwdsAt.IsZero() || now.Sub(state.cwdsAt) >= time.Minute)
		var pids []int32
		for _, proc := range r.procs {
			if refresh || (proc.Agent != "" && proc.Cwd == "") {
				pids = append(pids, proc.PID)
			}
		}
		if !refresh && len(pids) == 0 {
			return
		}
		sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })
		// Bound immediate lookups and rotate misses so inaccessible processes
		// cannot starve newly discovered agents. Full refreshes keep all pids.
		if !refresh && len(pids) > 200 {
			batch := make([]int32, 200)
			for i := range batch {
				batch[i] = pids[(state.cwdNext+i)%len(pids)]
			}
			state.cwdNext = (state.cwdNext + len(batch)) % len(pids)
			pids = batch
			sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })
		}
		r.cwdRan = true
		cwds, err := d.Plat.Cwds(ctx, pids)
		r.cwdErr = err
		if refresh {
			state.cwdsAt = now
		}
		if err != nil {
			return
		}
		if refresh {
			state.cwds = make(map[int32]cwdResult)
		}
		for _, proc := range r.procs {
			if path := cwds[proc.PID]; path != "" {
				state.cwds[proc.PID] = cwdResult{start: proc.StartTime, path: path}
			}
		}
		state.applyCwds(r.procs)
	})
	adapters.Wait()
	for _, result := range r.adapters {
		for _, o := range result.obs {
			if o.Adapter == "" {
				o.Adapter = result.name
			}
			r.observations = append(r.observations, o)
		}
	}
	r.original = make(map[int64]model.Session, len(ss))
	byKey := make(map[string]int, len(ss))
	for i, s := range ss {
		r.original[s.ID] = s
		byKey[sessionKey(s.Adapter, s.ExternalID)] = i
	}
	for _, o := range r.observations {
		if o.ExternalID == "" {
			continue
		}
		key := sessionKey(o.Adapter, o.ExternalID)
		i, ok := byKey[key]
		if !ok {
			i = len(ss)
			byKey[key] = i
			ss = append(ss, model.Session{})
		}
		mergeObs(&ss[i].SessionObs, o)
	}
	r.sessions = ss
	wg.Go(func() {
		<-procReady
		procs := r.procs
		if r.procErr != nil {
			// The pass reports both the scan error and any stored-read error.
			procs, _ = state.storedProcs(st.Procs)
		}
		reconcileClaude(r.sessions, r.observations, now)
		reconcileSessions(r.sessions, procs, d.Plat, now)
		// agy and Copilot without hooks use executable, PID identity, cwd and liveness.
		for _, proc := range procs {
			if (proc.Agent != "agy" && (proc.Agent != "copilot" || !d.Cfg.Adapters["copilot"].Enabled)) || (proc.Role != "interactive" && !(proc.Agent == "copilot" && proc.Role == "headless")) || !d.Plat.Alive(proc.PID, proc.StartTime) {
				continue
			}
			linked := false
			if proc.Agent == "copilot" {
				for _, s := range r.sessions {
					if s.Adapter == "copilot" && !strings.HasPrefix(s.ExternalID, "pid:") && s.PID == proc.PID && platform.SameStart(proc.StartTime, s.ProcStart) {
						linked = true
						break
					}
				}
			}
			if linked {
				continue
			}
			id := fmt.Sprintf("pid:%d:%d", proc.PID, proc.StartTime.UnixNano())
			i := -1
			for j, s := range r.sessions {
				if s.Adapter == proc.Agent && s.ExternalID == id {
					i = j
					break
				}
			}
			if i < 0 {
				i = len(r.sessions)
				r.sessions = append(r.sessions, model.Session{})
			}
			s := &r.sessions[i]
			s.SessionObs = model.SessionObs{Adapter: proc.Agent, ExternalID: id, Origin: proc.Role, PID: proc.PID, ProcStart: proc.StartTime, StartedAt: proc.StartTime, Cwd: proc.Cwd}
			if proc.Agent == "copilot" && proc.Cwd != "" {
				s.Title = filepath.Base(proc.Cwd)
			}
			s.EndedAt = nil
		}
		if p.Git {
			r.git, r.gitErr = collectGit(ctx, d, r.sessions, knownRepos, p.GitFull, now)
		}
	})
	wg.Wait()
	for _, a := range r.agents {
		if a.State != "blocked" {
			continue
		}
		found := false
		for _, s := range r.sessions {
			if s.Adapter == "claude" && s.ExternalID == a.ID {
				found = true
				break
			}
		}
		if !found {
			r.sessions = append(r.sessions, model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: a.ID, Cwd: a.Cwd, Origin: "background", Hosted: true, StartedAt: now}})
		}
	}
	return r, ctx.Err()
}

func collectGit(ctx context.Context, d Deps, ss []model.Session, known []model.Repo, full bool, now time.Time) ([]repoResult, error) {
	var cwds []string
	for _, s := range ss {
		if !strings.HasSuffix(s.Origin, "/temporary") && (s.EndedAt == nil || !s.LastActivityAt.Before(now.Add(-7*24*time.Hour))) {
			cwds = append(cwds, s.Cwd)
		}
	}
	for _, repo := range known {
		cwds = append(cwds, repo.Path)
	}
	paths, err := gitscan.DiscoverRepos(cwds, d.Cfg.Roots, d.Cfg.RootDepth, d.Cfg.Exclude)
	errs := []error{err}
	scanner := d.Git
	if scanner == nil {
		scanner = gitscan.NewScanner(4)
	}
	results := make([]repoResult, len(paths))
	scanErrs := make([]error, len(paths))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(4, len(paths)) {
		wg.Go(func() {
			for i := range jobs {
				results[i].path = paths[i]
				results[i].worktrees, scanErrs[i] = scanner.Scan(ctx, paths[i], full)
				results[i].err = scanErrs[i]
			}
		})
	}
	for i := range paths {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	errs = append(errs, scanErrs...)
	if err == nil {
		present := make(map[string]bool, len(paths))
		for _, path := range paths {
			present[path] = true
		}
		// Missing repositories (or removed Git metadata) have no current
		// checkouts. Keep their repo/session history, but retire inventory.
		for _, repo := range known {
			if !present[repo.Path] {
				results = append(results, repoResult{path: repo.Path})
			}
		}
	}
	return results, errors.Join(errs...)
}

func writeGit(tx *store.Tx, repos []repoResult, scanErr error, source func(string, error) error, report *PassReport) error {
	for _, repo := range repos {
		if len(repo.worktrees) == 0 && repo.err != nil {
			continue
		}
		id, err := tx.UpsertRepo(model.Repo{Path: repo.path, Name: filepath.Base(repo.path)})
		if err != nil {
			return err
		}
		report.Repos++
		for _, w := range repo.worktrees {
			w.RepoID = id
			if _, err := tx.UpsertWorktree(w); err != nil {
				return err
			}
		}
		// Only a complete scan can prove absence. A failed status/log read
		// returns a partial list and must retain the last known inventory.
		if repo.err == nil {
			previous, err := tx.Worktrees(id)
			if err != nil {
				return err
			}
			present := make(map[string]bool, len(repo.worktrees))
			for _, w := range repo.worktrees {
				present[w.Path] = true
			}
			for _, w := range previous {
				if !present[w.Path] {
					if err := tx.DeleteWorktree(w.ID); err != nil {
						return err
					}
				}
			}
		}
	}
	return source("git", scanErr)
}

// Reconcile liveness before git discovery and persistence, using the same cwd
// sample so newly linked or resumed sessions are included by both.
func reconcileSessions(ss []model.Session, procs []model.Proc, plat platform.Platform, now time.Time) {
	for i := range ss {
		s := &ss[i]
		if (s.Adapter == "copilot" || s.Adapter == "claude") && s.RegistryStatus == "ended" {
			if s.EndedAt == nil {
				at := s.LastActivityAt
				s.EndedAt = &at
			}
			continue
		}
		if s.PID != 0 && !plat.Alive(s.PID, s.ProcStart) {
			s.PID = 0
			if s.Adapter == "claude" && s.RegistryStatus != "" {
				s.Hosted = false
			}
		}
	}
	linkCodex(ss, procs, plat)
	for i := range ss {
		s := &ss[i]
		if (s.Adapter == "copilot" || s.Adapter == "claude") && s.RegistryStatus == "ended" {
			continue
		}
		if s.PID != 0 || s.Hosted && now.Sub(s.LastActivityAt) <= 24*time.Hour {
			s.EndedAt = nil
			if s.Label == "dead" || s.Label == "ended" {
				s.Label = ""
			}
		} else if s.EndedAt == nil {
			ended := now
			if s.LastActivityAt.After(ended) {
				ended = s.LastActivityAt
			}
			s.EndedAt = &ended
		}
	}
}

// A registry identifies the current conversation of a process, even after
// /clear or /resume. A transcript in the same directory is not ownership proof.
func reconcileClaude(ss []model.Session, observations []model.SessionObs, now time.Time) {
	current := map[int32]model.SessionObs{}
	ambiguous := map[int32]bool{}
	for _, o := range observations {
		if o.Adapter == "claude" && o.PID != 0 && o.RegistryStatus != "" {
			if previous, ok := current[o.PID]; ok && previous.ExternalID != o.ExternalID {
				ambiguous[o.PID] = true
			}
			current[o.PID] = o
		}
	}
	for i := range ss {
		s := &ss[i]
		o, ok := current[s.PID]
		if s.Adapter != "claude" || !ok || ambiguous[s.PID] || !platform.SameStart(s.ProcStart, o.ProcStart) || s.ExternalID == o.ExternalID {
			continue
		}
		s.PID, s.Hosted, s.TurnOpen = 0, false, false
		s.RegistryStatus, s.RegistryUpdatedAt = "ended", now
		if s.EndedAt == nil {
			at := now
			s.EndedAt = &at
		}
	}
}
