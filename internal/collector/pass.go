// Package collector gathers source observations and derives the stored view.
package collector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/gitscan"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/scrub"
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

// Deps supplies collection sources, configuration, and optional shared state.
type Deps struct {
	Cfg        config.Config
	Plat       platform.Platform
	Adapters   []adapter.Adapter
	Sigs       *signatures.Set
	Git        *gitscan.Scanner
	SpoolDir   string
	Now        func() time.Time
	State      *State
	spoolSince time.Time
	snapshot   bool
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}

// Plan selects the sources whose timers are due. Adapters and spool always run.
// Sockets probes orphan candidates' connections; only repeated passes can use
// that to verify idleness, so a one-shot snapshot leaves it off and
// `orphans kill` verifies on demand instead.
type Plan struct{ Procs, Machine, Git, GitFull, Cwds, Sockets bool }

// PassReport summarizes a committed pass. Source errors do not abort collection.
type PassReport struct {
	ObservedAt                                      time.Time
	Sessions, Procs, Samples, Repos, Orphans, Inbox int
	SourceErrors                                    map[string]string
}

// Pass gathers and derives one atomic update. Only storage failures abort it.
func Pass(ctx context.Context, d Deps, st *store.Store, p Plan) (PassReport, error) {
	state := d.State
	if state == nil {
		state = NewState()
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	work := state.copy()
	now := d.now()
	r := PassReport{ObservedAt: now, SourceErrors: make(map[string]string)}
	collected, err := collectSources(ctx, d, st, p, work, now)
	if err != nil {
		return r, err
	}
	err = st.Tx(ctx, func(tx *store.Tx) error {
		source := func(name string, err error) error {
			if err != nil {
				message := scrub.Scrub(err.Error())
				r.SourceErrors[name] = message
				return tx.SetSourceError(name, message, now)
			}
			return tx.SetSourceOK(name, now)
		}
		hooks, err := drainSpool(d, tx, now, source)
		if err != nil {
			return err
		}

		for _, result := range collected.adapters {
			if err := source(result.name, result.err); err != nil {
				return err
			}
			for path, offset := range result.offsets.next {
				if err := tx.SetOffset(path, offset); err != nil {
					return err
				}
			}
			for _, w := range result.warnings {
				detail := w.Detail
				if w.Kind == "untested-version" {
					if _, v, ok := strings.Cut(detail, ": "); ok {
						detail = v
					}
				}
				if err := tx.SetMeta("warn:"+result.name+":"+w.Kind, scrub.Scrub(detail)); err != nil {
					return err
				}
			}
		}
		procs, err := tx.Procs()
		if err != nil {
			return err
		}
		procsOK := false
		if p.Procs {
			fresh, err := collected.procs, collected.procErr
			if e := source("procs", err); e != nil {
				return e
			}
			if err == nil {
				procs = fresh
				procsOK = true
				for i, cur := range procs {
					procs[i].CPUPercent = platform.CPUPercent(work.prev[cur.PID], cur, collected.procAt.Sub(work.prevAt))
				}
				work.prev = make(map[int32]model.Proc, len(procs))
				for _, proc := range procs {
					work.prev[proc.PID] = proc
				}
				work.prevAt = collected.procAt
			}
		}
		if collected.cwdRan {
			if err := source("cwds", collected.cwdErr); err != nil {
				return err
			}
		}
		work.applyCwds(procs)
		if p.Machine {
			sample, err := collected.sample, collected.machineErr
			if e := source("machine", err); e != nil {
				return e
			}
			if err == nil {
				if sample.TS.IsZero() {
					sample.TS = now
				}
				if err := tx.InsertSample(sample); err != nil {
					return err
				}
				r.Samples = 1
				m, err := tx.Machine()
				if err != nil && !errors.Is(err, store.ErrNotFound) {
					return err
				}
				if m.FirstSeen.IsZero() {
					m.FirstSeen = now
				}
				m.Hostname, _ = os.Hostname()
				m.OS, m.Arch = runtime.GOOS, runtime.GOARCH
				m.Cores, m.MemTotal, m.LastSeen = sample.Cores, sample.MemTotal, now
				if err := tx.UpsertMachine(m); err != nil {
					return err
				}
			}
		}

		baseline := collected.baseline
		if collected.first {
			if err := tx.SetMeta("baseline_at", baseline.Format(time.RFC3339Nano)); err != nil {
				return err
			}
		}
		ss := collected.sessions
		original := collected.original
		byKey := make(map[string]int, len(ss))
		for i, s := range ss {
			byKey[sessionKey(s.Adapter, s.ExternalID)] = i
		}

		for i := range ss {
			s := &ss[i]
			s.Title = scrub.Scrub(s.Title)
			if d.Cfg.CaptureSnippets() {
				s.LastPrompt = scrub.Snippet(s.LastPrompt)
			} else {
				s.LastPrompt = ""
			}
			for j := range s.Completions {
				if d.Cfg.CaptureSnippets() {
					s.Completions[j].Text = scrub.Snippet(s.Completions[j].Text)
				} else {
					s.Completions[j].Text = ""
				}
			}

			// Allocate stable IDs before files, handoffs, families, and inbox rows.
			if s.ID == 0 {
				s.ID, err = tx.UpsertSession(*s)
				if err != nil {
					return err
				}
			}
			if err := tx.ReplaceSessionFiles(s.ID, s.FilesTouched); err != nil {
				return err
			}
			for _, c := range s.Completions {
				if s.Adapter == "codex" && c.Text != "" {
					if err := tx.InsertHandoff(s.ID, c.TS, c.Text); err != nil {
						return err
					}
				}
			}
		}
		for _, h := range hooks {
			if h.Event == "Stop" && h.Message != "" {
				if i, ok := byKey[sessionKey(h.Adapter, h.SessionID)]; ok {
					if err := tx.InsertHandoff(ss[i].ID, h.TS, h.Message); err != nil {
						return err
					}
				}
			}
		}
		idx := derive.NewProcIndex(procs)
		// Inbox grouping needs parent IDs before labels consume the unseen items.
		parents := derive.LinkParents(ss, idx)
		for _, item := range derive.InboxItems(ss, parents, hooks, baseline) {
			if err := tx.UpsertInboxItem(item); err != nil {
				return err
			}
			r.Inbox++
		}
		items, err := tx.Inbox(time.Time{}, false)
		if err != nil {
			return err
		}
		unseen := make(map[int64]bool)
		for _, item := range items {
			if item.Kind == "finished" {
				unseen[item.SessionID] = true
			}
		}
		allEvents, err := tx.LatestEvents(time.Time{})
		if err != nil {
			return err
		}
		eventsBySession := make(map[string][]model.Event)
		for _, e := range allEvents {
			key := sessionKey(e.Adapter, e.ExternalID)
			eventsBySession[key] = append(eventsBySession[key], e)
		}
		for i := range ss {
			s := &ss[i]
			events := eventsBySession[sessionKey(s.Adapter, s.ExternalID)]
			in := derive.StateInput{Obs: s.SessionObs, LastUserPrompt: s.LastPromptAt, Unseen: unseen[s.ID]}
			if proc, ok := idx.Get(s.PID); ok && (s.ProcStart.IsZero() || derive.SameStart(proc.StartTime, s.ProcStart)) {
				in.Proc = &proc
			}
			for _, e := range events {
				switch e.Kind {
				case "Notification":
					if in.LastNotification == nil || e.TS.After(*in.LastNotification) {
						ts := e.TS
						in.LastNotification = &ts
					}
				case "Stop":
					if in.LastStop == nil || e.TS.After(*in.LastStop) {
						ts := e.TS
						in.LastStop = &ts
					}
				}
			}
			s.Label, s.Heuristic, s.Evidence = derive.Label(in, now, derive.Thresholds{StuckAfter: d.Cfg.StuckAfter})
		}
		parents = derive.LinkParents(ss, idx)
		for i := range ss {
			ss[i].ParentID = nil
			if id, ok := parents[ss[i].ID]; ok {
				parent := id
				ss[i].ParentID = &parent
			}
		}
		if p.Git {
			if err := writeGit(tx, collected.git, collected.gitErr, source, &r); err != nil {
				return err
			}
			if p.GitFull && r.SourceErrors["git"] == "" {
				work.gitFullAt = now
			}
		}
		if err := mapRepos(tx, ss); err != nil {
			return err
		}
		if procsOK {
			spawn := derive.SpawnedBySession(idx, ss)
			memory := make(map[string]int64)
			for i, proc := range procs {
				key := fmt.Sprintf("%d:%d", proc.PID, proc.StartTime.Unix())
				id := work.spawn[key]
				if id == 0 {
					id = spawn[proc.PID]
				}
				if id != 0 {
					memory[key] = id
				}
				procs[i].SessionID = id
				for _, s := range ss {
					if s.PID == proc.PID {
						procs[i].SessionID = s.ID
						break
					}
				}
			}
			work.spawn = memory
			old, err := tx.Orphans()
			if err != nil {
				return err
			}
			prev := make(map[string]model.Orphan)
			for _, o := range old {
				prev[o.ID] = o
			}
			orphans := derive.FindOrphans(idx, ss, d.Sigs, now, d.Cfg.OrphanMinAge, prev, derive.WithSpawnMemory(memory))
			var peers map[int32]bool
			if probe, ok := d.Plat.(platform.SocketProber); ok && p.Sockets && len(orphans) > 0 {
				var pids []int32
				for _, o := range orphans {
					pids = append(pids, o.PIDs...)
				}
				sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })
				var err error
				peers, err = probe.ExternalPeers(ctx, pids)
				if err != nil {
					peers = nil
				}
				if e := source("sockets", err); e != nil {
					return e
				}
			}
			orphans = derive.FindOrphans(idx, ss, d.Sigs, now, d.Cfg.OrphanMinAge, prev, derive.WithSpawnMemory(memory), derive.WithExternalPeers(peers))
			if err := tx.ReplaceOrphans(orphans); err != nil {
				return err
			}
			r.Orphans = len(orphans)
			if err := tx.ReplaceProcs(procs); err != nil {
				return err
			}
		}
		for _, s := range ss {
			before := original[s.ID]
			current := s
			current.Completions, current.FilesTouched = nil, nil
			if reflect.DeepEqual(before, current) {
				continue
			}
			if _, err := tx.UpsertSession(s); err != nil {
				return err
			}
		}
		r.Sessions, r.Procs = len(ss), len(procs)
		return tx.SetMeta("last_pass_at", now.Format(time.RFC3339Nano))
	})
	if err == nil {
		state.prev, state.prevAt = work.prev, work.prevAt
		state.cwds, state.cwdsAt = work.cwds, work.cwdsAt
		state.cwdNext = work.cwdNext
		state.spawn, state.gitFullAt = work.spawn, work.gitFullAt
	}
	return r, err
}

func sessionKey(a, id string) string { return a + "\x00" + id }

func drainSpool(d Deps, tx *store.Tx, now time.Time, source func(string, error) error) ([]model.HookEvent, error) {
	offsets := make(map[string]model.Offset)
	entries, err := os.ReadDir(d.SpoolDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, source("spool", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") || !spool.RecentDay(entry.Name(), d.spoolSince) {
			continue
		}
		o, _, err := tx.GetOffset("spool/" + entry.Name())
		if err != nil {
			return nil, err
		}
		offsets[entry.Name()] = o
	}
	events, next, err := spool.Drain(d.SpoolDir, offsets, d.spoolSince)
	if err := source("spool", err); err != nil {
		return nil, err
	}
	for name, o := range next {
		if err := tx.SetOffset("spool/"+name, o); err != nil {
			return nil, err
		}
	}
	var hooks []model.HookEvent
	for _, ev := range events {
		if ev.Event == "ack" {
			if _, err := tx.AckInbox(ev.Message, ev.TS); err != nil {
				return nil, err
			}
			if err := tx.SetMeta("ack:"+ev.Message, now.Format(time.RFC3339Nano)); err != nil {
				return nil, err
			}
			continue
		}
		if d.Cfg.CaptureSnippets() {
			ev.Message = scrub.Snippet(ev.Message)
		} else {
			ev.Message = ""
		}
		if err := tx.InsertEvent(model.Event{Adapter: ev.Adapter, ExternalID: ev.SessionID, TS: ev.TS, Kind: ev.Event, Summary: ev.Message}); err != nil {
			return nil, err
		}
		hooks = append(hooks, ev)
	}
	return hooks, nil
}

func mergeObs(dst *model.SessionObs, src model.SessionObs) {
	dst.Adapter, dst.ExternalID = src.Adapter, src.ExternalID
	// One identity per path (Review Focus 3): /tmp vs /private/tmp, /var vs
	// /private/var and symlinked repos must compare equal to git's paths.
	if src.Cwd != "" {
		src.Cwd = gitscan.NormalizePath(src.Cwd)
	}
	if len(src.FilesTouched) > 0 {
		files := make([]model.FileTouch, len(src.FilesTouched))
		for i, f := range src.FilesTouched {
			files[i] = model.FileTouch{Path: gitscan.NormalizePath(f.Path), TS: f.TS}
		}
		src.FilesTouched = files
	}
	for _, pair := range [][2]*string{{&dst.Origin, &src.Origin}, {&dst.Version, &src.Version}, {&dst.Cwd, &src.Cwd}, {&dst.Branch, &src.Branch}, {&dst.Title, &src.Title}, {&dst.LastPrompt, &src.LastPrompt}, {&dst.RegistryStatus, &src.RegistryStatus}, {&dst.SourcePath, &src.SourcePath}} {
		if *pair[1] != "" {
			*pair[0] = *pair[1]
		}
	}
	if src.PID != 0 {
		dst.PID = src.PID
	}
	if !src.ProcStart.IsZero() {
		dst.ProcStart = src.ProcStart
	}
	if src.Adapter == "claude" && src.RegistryStatus != "" {
		dst.PID, dst.ProcStart, dst.RegistryStatus = src.PID, src.ProcStart, src.RegistryStatus
	}
	if !src.StartedAt.IsZero() {
		dst.StartedAt = src.StartedAt
	}
	// A header-only Codex delta must not close a previously open turn or move
	// activity backwards. New activity carries the adapter's folded turn state.
	if src.LastActivityAt.After(dst.LastActivityAt) {
		dst.LastActivityAt = src.LastActivityAt
		dst.TurnOpen = src.TurnOpen
	}
	if src.TurnOpen {
		dst.TurnOpen = true
	}
	if src.LastPromptAt.After(dst.LastPromptAt) {
		dst.LastPromptAt = src.LastPromptAt
	}
	if src.Hosted {
		dst.Hosted = true
	}
	dst.Completions = append(dst.Completions, src.Completions...)
	dst.FilesTouched = append(dst.FilesTouched, src.FilesTouched...)
}

func linkCodex(ss []model.Session, procs []model.Proc, plat platform.Platform) {
	used := make(map[int32]bool)
	var order []int
	for i, s := range ss {
		if s.PID != 0 {
			used[s.PID] = true
		}
		if s.Adapter == "codex" && s.PID == 0 && !s.Hosted && s.Cwd != "" && !s.StartedAt.IsZero() {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := ss[order[i]], ss[order[j]]
		if a.StartedAt.Equal(b.StartedAt) {
			return a.ExternalID < b.ExternalID
		}
		return a.StartedAt.After(b.StartedAt)
	})
	for _, i := range order {
		s := &ss[i]
		var best *model.Proc
		for j := range procs {
			p := &procs[j]
			if used[p.PID] || p.Agent != "codex" || (p.Role != "interactive" && p.Role != "headless") || p.Cwd == "" || p.StartTime.IsZero() || p.StartTime.After(s.StartedAt.Add(5*time.Second)) || !plat.Alive(p.PID, p.StartTime) {
				continue
			}
			if gitscan.NormalizePath(p.Cwd) != gitscan.NormalizePath(s.Cwd) {
				continue
			}
			if best == nil || p.StartTime.After(best.StartTime) || p.StartTime.Equal(best.StartTime) && p.PID < best.PID {
				best = p
			}
		}
		if best != nil {
			s.PID, s.ProcStart = best.PID, best.StartTime
			used[best.PID] = true
		}
	}
}

func mapRepos(tx *store.Tx, ss []model.Session) error {
	ws, err := tx.Worktrees(0)
	if err != nil {
		return err
	}
	paths := make(map[string]string)
	normalize := func(path string) string {
		if n, ok := paths[path]; ok {
			return n
		}
		n := gitscan.NormalizePath(path)
		paths[path] = n
		return n
	}
	for i := range ws {
		ws[i].Path = normalize(ws[i].Path)
	}
	for i := range ss {
		s := &ss[i]
		s.RepoID, s.WorktreeID = nil, nil
		if s.Cwd == "" {
			continue
		}
		cwd := normalize(s.Cwd)
		best := -1
		for _, w := range ws {
			path := w.Path
			if len(path) > best && (cwd == path || strings.HasPrefix(cwd, strings.TrimSuffix(path, string(filepath.Separator))+string(filepath.Separator))) {
				repo, worktree := w.RepoID, w.ID
				s.RepoID, s.WorktreeID = &repo, &worktree
				best = len(path)
			}
		}
	}
	return nil
}

// Snapshot gathers a disposable memory view, sampling CPU across source collection without
// opening a persistent database or mutating the spool.
func Snapshot(ctx context.Context, d Deps) (*store.Store, error) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		return nil, err
	}
	d.State = NewState()
	d.spoolSince = d.now().Add(-7 * 24 * time.Hour)
	d.snapshot = true
	started := time.Now()
	procs, seedErr := d.Plat.Procs(ctx)
	if seedErr == nil {
		for _, p := range procs {
			d.State.prev[p.PID] = p
		}
		d.State.prevAt = started
	}
	if _, err := Pass(ctx, d, st, Plan{Procs: true, Machine: true, Git: true, GitFull: true, Cwds: true}); err != nil {
		st.Close()
		return nil, err
	}
	return st, nil
}
