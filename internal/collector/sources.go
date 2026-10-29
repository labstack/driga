package collector

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/labstack/driga/internal/adapter"
	attention "github.com/labstack/driga/internal/attention"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/gitscan"
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
	agents         []attention.Agent
	sessions       []model.Session
	processAliases map[int64]int
	internalChats  map[string]bool
	original       map[int64]model.Session
	procs          []model.Proc
	procErr        error
	procAt         time.Time
	sample         model.MachineSample
	machineErr     error
	adapters       []adapterResult
	observations   []model.SessionObs
	first          bool
	baseline       time.Time
	cwdRan         bool
	cwdErr         error
	git            []repoResult
	gitErr         error
	feedEnabled    bool
	spool          spoolResult
	peers          map[int32]bool
	socketErr      error
	subreaper      int32
	paths          map[string]string
	logs           map[string]logBaseline
	jumps          map[string]*model.InboxJump
}

func collectSources(ctx context.Context, d Deps, st *store.Store, p Plan, state *State, now time.Time) (sourceResults, error) {
	r := sourceResults{baseline: now}
	var err error
	r.feedEnabled, err = collectFeedEnabled(d, st.DataDir())
	if err != nil {
		return r, err
	}
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
	conversations, err := st.InternalChatSessions()
	if err != nil {
		return r, err
	}
	r.internalChats = make(map[string]bool, len(conversations))
	for _, conversation := range conversations {
		if conversation != "" {
			r.internalChats[conversation] = true
		}
	}
	kept := ss[:0]
	for _, session := range ss {
		if session.Adapter != "claude" || !r.internalChats[session.ExternalID] {
			kept = append(kept, session)
		}
	}
	ss = kept
	r.spool = collectSpool(d, offsets)
	var knownRepos []model.Repo
	if p.Git || p.GitIdentity {
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
			result.obs, result.warnings, result.err = a.Collect(ctx, adapter.Env{Home: config.ExpandHome("~"), Now: now, CaptureSnippets: d.Cfg.CaptureSnippets(), Offsets: result.offsets, Alive: d.Plat.Alive, Baseline: r.first})
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
				sig, _ := d.classifier.Match(r.procs[i])
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
	r.original = make(map[int64]model.Session, len(ss))
	knownClaude := make(map[string]bool)
	for _, s := range ss {
		r.original[s.ID] = s
		if claudeConversationClaimed(s.SessionObs) {
			knownClaude[s.ExternalID] = true
		}
	}
	for _, h := range spoolHooks(d, r.spool.events) {
		if claudeTurnHookClaimed(h) {
			knownClaude[h.SessionID] = true
		}
	}
	backgroundRegistries := make(map[string]bool)
	var observations []model.SessionObs
	for _, result := range r.adapters {
		for _, o := range result.obs {
			if o.Adapter == "" {
				o.Adapter = result.name
			}
			if o.Adapter == "claude" && r.internalChats[o.ExternalID] {
				continue
			}
			if o.Adapter == "claude" && strings.TrimSuffix(o.Origin, "/temporary") == "background" && o.RegistryStatus != "" {
				backgroundRegistries[o.ExternalID] = true
			}
			if claudeConversationClaimed(o) {
				o.ConversationClaimed = true
				knownClaude[o.ExternalID] = true
			}
			observations = append(observations, o)
		}
	}
	// Even an unclaimed spare can replace a conversation's registry identity.
	// Detach its predecessor before deciding whether to admit the replacement.
	reconcileClaude(ss, observations, now)
	for i := range ss {
		s := &ss[i]
		if s.Adapter != "claude" {
			continue
		}
		s.ConversationClaimed = knownClaude[s.ExternalID]
		storedBackground := strings.TrimSuffix(s.Origin, "/temporary") == "background" && s.RegistryStatus != ""
		if !s.ConversationClaimed && (storedBackground || backgroundRegistries[s.ExternalID]) {
			// Retire registry-only spares persisted by older collectors. Retain
			// their historical row, but never their live process ownership.
			// Main-era rows used interactive/headless origins for bg registries;
			// the incoming identity must retire those before admission filtering.
			s.PID, s.Hosted, s.TurnOpen = 0, false, false
			if s.RegistryStatus != "ended" {
				s.RegistryStatus, s.RegistryUpdatedAt = "ended", now
			}
			if s.EndedAt == nil {
				at := now
				s.EndedAt = &at
			}
		}
	}
	for _, o := range observations {
		// A pre-warmed bg spare has a sessionId and an idle registry too.
		// Only positive evidence (including a persisted claim) admits it.
		if o.Adapter == "claude" && strings.TrimSuffix(o.Origin, "/temporary") == "background" && !knownClaude[o.ExternalID] {
			continue
		}
		r.observations = append(r.observations, o)
	}
	byKey := make(map[string]int, len(ss))
	for i, s := range ss {
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
		reconcileSessions(r.sessions, procs, d.Plat, now)
		r.sessions = processSessions(r.sessions, procs, d)
		r.sessions = hookSessions(r.sessions, spoolHooks(d, r.spool.events), procs)
		copilotCapturePolicy(r.sessions, d.Cfg.CaptureText)
		reconcileCopilot(r.sessions, d.Plat, now)
		r.processAliases = retireProcessAliases(r.sessions, procs, now)
		if p.Git || p.GitIdentity {
			mode := gitscan.Cached
			if p.GitFull {
				mode = gitscan.Full
			}
			if p.GitIdentity {
				mode = gitscan.Identity
			}
			r.git, r.gitErr = collectGit(ctx, d, r.sessions, knownRepos, mode, now)
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
	// All remaining filesystem and subprocess evidence is captured before the
	// write transaction; readers keep using the last committed frame.
	sessions := r.sessions
	r.logs = collectLogBaselines(sessions, state.logs)
	procs := r.procs
	if r.procErr != nil {
		procs, err = state.storedProcs(st.Procs)
		if err != nil {
			return r, err
		}
	}
	r.jumps = collectInboxJumps(ctx, d, sessions, procs)
	r.paths = make(map[string]string)
	for _, s := range sessions {
		r.paths[s.Cwd] = gitscan.NormalizePath(s.Cwd)
	}
	ws, err := st.Worktrees(0)
	if err != nil {
		return r, err
	}
	for _, w := range ws {
		r.paths[w.Path] = gitscan.NormalizePath(w.Path)
	}
	for _, repo := range r.git {
		for _, w := range repo.worktrees {
			r.paths[w.Path] = w.Path
		}
	}
	if finder, ok := d.Plat.(platform.SubreaperFinder); ok {
		if pid, ok := finder.UserSubreaper(); ok {
			r.subreaper = pid
		}
	}
	if p.Procs && r.procErr == nil && p.Sockets {
		r.peers, r.socketErr = collectSocketPeers(ctx, d, st, state, r.procs, sessions, r.procAt, now, r.subreaper, spoolHooks(d, r.spool.events), r.processAliases)
	}
	return r, ctx.Err()
}

func claudeConversationClaimed(o model.SessionObs) bool {
	return o.Adapter == "claude" && !strings.HasPrefix(o.ExternalID, "pid:") && (o.ConversationClaimed || o.RegistryStatus == "busy" || o.SourcePath != "")
}

// Lifecycle hooks can fire for pre-warmed spares. Only turn activity proves a
// conversation was claimed; keep this list aligned with migration 0006.
func claudeTurnHookClaimed(h model.HookEvent) bool {
	if h.Adapter != "claude" || h.SessionID == "" || strings.HasPrefix(h.SessionID, "pid:") {
		return false
	}
	switch h.Event {
	case "UserPromptSubmit", "Stop", "PreToolUse", "PostToolUse", "PostToolUseFailure", "PermissionRequest":
		return true
	}
	return false
}

func collectGit(ctx context.Context, d Deps, ss []model.Session, known []model.Repo, mode gitscan.ScanMode, now time.Time) ([]repoResult, error) {
	var cwds []string
	for _, s := range ss {
		if !strings.HasSuffix(s.Origin, "/temporary") && (s.EndedAt == nil || !s.LastActivityAt.Before(now.Add(-7*24*time.Hour))) {
			cwds = append(cwds, s.Cwd)
		}
	}
	// Known repositories are exact inventory roots. A removed checkout must
	// not be rediscovered as an unrelated repository in its parent directory.
	roots := append([]string(nil), d.Cfg.Roots...)
	for _, repo := range known {
		roots = append(roots, repo.Path)
	}
	paths, err := gitscan.DiscoverRepos(cwds, roots, d.Cfg.RootDepth, d.Cfg.Exclude)
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
				results[i].worktrees, scanErrs[i] = scanner.Scan(ctx, paths[i], mode)
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
	if err == nil && ctx.Err() == nil {
		scanner.Prune(paths)
	}
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
		if s.Adapter != "claude" || strings.HasPrefix(s.ExternalID, "pid:") || !ok || ambiguous[s.PID] || !platform.SameStart(s.ProcStart, o.ProcStart) || s.ExternalID == o.ExternalID {
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

func spoolHooks(d Deps, events []model.HookEvent) []model.HookEvent {
	var hooks []model.HookEvent
	for _, ev := range events {
		if ev.Event != "summary" && ev.Event != "ack" && (ev.Adapter != "copilot" || d.Cfg.Adapters["copilot"].Enabled) {
			hooks = append(hooks, prepareHook(d, ev))
		}
	}
	return hooks
}

func collectSocketPeers(ctx context.Context, d Deps, st *store.Store, state *State, procs []model.Proc, ss []model.Session, procAt, now time.Time, subreaper int32, hooks []model.HookEvent, aliases map[int64]int) (map[int32]bool, error) {
	probe, ok := d.Plat.(platform.SocketProber)
	if !ok {
		return nil, nil
	}
	sampled := append([]model.Proc(nil), procs...)
	for i, p := range sampled {
		sampled[i].CPUPercent = platform.CPUPercent(state.prev[p.PID], p, procAt.Sub(state.prevAt))
	}
	idx, err := derive.NewProcIndexForOS(sampled, platform.ProcessOS(d.Plat))
	if err != nil {
		return nil, err
	}
	old, err := st.Orphans()
	if err != nil {
		return nil, err
	}
	prev := make(map[string]model.Orphan, len(old))
	for _, o := range old {
		prev[o.ID] = o
	}
	// Fold the same lifecycle evidence as labelSession before selecting probe
	// PIDs. Expanding this set would treat sockets owned by live helpers as
	// internal, and could incorrectly verify an orphan as idle.
	events, err := st.LatestEvents(time.Time{})
	if err != nil {
		return nil, err
	}
	// New hook rows win equal-timestamp ties, in reverse insertion order.
	hookEvents := make([]model.Event, 0, len(hooks)+len(events))
	for i := len(hooks) - 1; i >= 0; i-- {
		h := hooks[i]
		hookEvents = append(hookEvents, model.Event{Adapter: h.Adapter, ExternalID: h.SessionID, TS: h.TS, Kind: hookKind(h), Summary: h.Message})
	}
	events = append(hookEvents, events...)
	sort.SliceStable(events, func(i, j int) bool { return events[i].TS.After(events[j].TS) })
	bySession := make(map[string][]model.Event)
	for _, e := range events {
		key := sessionKey(e.Adapter, e.ExternalID)
		bySession[key] = append(bySession[key], e)
	}
	sessions := append([]model.Session(nil), ss...)
	for i := range sessions {
		s := &sessions[i]
		in := derive.StateInput{Obs: s.SessionObs, LastUserPrompt: s.LastPromptAt}
		if p, ok := idx.Get(s.PID); ok && (s.ProcStart.IsZero() || derive.SameStart(p.StartTime, s.ProcStart)) {
			in.Proc = &p
		}
		if err := intakeInput(st, *s, bySession[sessionKey(s.Adapter, s.ExternalID)], &in, now); err != nil {
			return nil, err
		}
		mixSession(s, in, d.Cfg.StuckAfter, now)
		// New rows receive real IDs in the write transaction. Private IDs keep
		// their process ownership distinct while planning the probe.
		if s.ID == 0 {
			s.ID = -int64(i + 1)
		}
	}
	spawn := derive.SpawnedBySession(idx, sessions)
	memory := make(map[string]int64)
	for _, p := range sampled {
		key := fmt.Sprintf("%d:%d", p.PID, p.StartTime.Unix())
		id := state.spawn[key]
		if replacement, ok := aliases[id]; ok {
			id = sessions[replacement].ID
		}
		if id == 0 {
			id = spawn[p.PID]
		}
		if id != 0 {
			memory[key] = id
		}
	}
	candidates := derive.FindOrphans(idx, sessions, d.Sigs, now, d.Cfg.OrphanMinAge, prev, derive.WithSpawnMemory(memory), derive.WithSubreaper(subreaper), derive.WithClassifier(d.classifier))
	var pids []int32
	for _, o := range candidates {
		pids = append(pids, o.PIDs...)
	}
	if len(pids) == 0 {
		return nil, nil
	}
	sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })
	peers, err := probe.ExternalPeers(ctx, pids)
	if err != nil {
		return nil, err
	}
	return peers, nil
}
