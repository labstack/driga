// Package collector gathers source observations and derives the stored view.
package collector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/labstack/driga/internal/pathx"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/gitscan"
	attention "github.com/labstack/driga/internal/inbox"
	"github.com/labstack/driga/internal/jsonx"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/scrub"
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

// Deps supplies collection sources, configuration, and optional shared state.
type Deps struct {
	InboxRunner attention.Runner
	Notifier    attention.Notifier
	Cfg         config.Config
	Plat        platform.Platform
	Adapters    []adapter.Adapter
	Sigs        *signatures.Set
	Git         interface {
		Scan(context.Context, string, gitscan.ScanMode) ([]model.Worktree, error)
		Prune([]string)
	}
	SpoolDir   string
	Now        func() time.Time
	State      *State
	spoolSince time.Time
	snapshot   bool
	clock      daemonClock
	classifier *derive.Classifier
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
type Plan struct{ Procs, Machine, Git, GitFull, GitIdentity, Cwds, Sockets bool }

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
	if p.Procs {
		d.classifier = derive.NewClassifier(d.Sigs, len(work.prev))
	}
	now := d.now()
	r := PassReport{ObservedAt: now, SourceErrors: make(map[string]string)}
	collected, err := collectSources(ctx, d, st, p, work, now)
	if err != nil {
		return r, err
	}
	err = st.Tx(ctx, func(tx *store.Tx) error {
		share := feed.EffectiveShare(feed.ShareText(d.Cfg.Fleet.ShareText), d.Cfg.CaptureText)
		feedEnabled, err := configurePassFeed(d, tx, collected.feedEnabled)
		if err != nil {
			return err
		}
		var beforeFeed map[string]passFeedRow
		if feedEnabled {
			beforeFeed, err = passFeedRows(tx, now, share)
		}
		if err != nil {
			return err
		}
		source := func(name string, err error) error {
			if err != nil {
				message := scrub.Scrub(err.Error())
				r.SourceErrors[name] = message
				return tx.SetSourceError(name, message, now)
			}
			return tx.SetSourceOK(name, now)
		}
		hooks, err := writeSpool(d, tx, collected.spool, now, source)
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
				if w.Kind == "untested_version" {
					if _, v, ok := strings.Cut(detail, ": "); ok {
						detail = v
					}
				}
				if err := tx.SetMeta("warn:"+result.name+":"+w.Kind, scrub.Scrub(detail)); err != nil {
					return err
				}
			}
		}
		var procs []model.Proc
		previousProcs := work.prev
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
		if !procsOK {
			stored, err := work.storedProcs(tx.Procs)
			if err != nil {
				return err
			}
			procs = stored
		}
		if collected.cwdRan {
			if err := source("cwds", collected.cwdErr); err != nil {
				return err
			}
		}
		work.applyCwds(procs)
		// Every pass records process provenance, even if collection is not due
		// or the machine/process source fails this pass.
		m, err := tx.Machine()
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		m.OS, m.Arch = platform.ProcessOS(d.Plat), runtime.GOARCH
		if m.OS == "" {
			return fmt.Errorf("process source OS is required")
		}
		if err := tx.UpsertMachine(m); err != nil {
			return err
		}
		if procsOK {
			due, err := tx.MCPObservationDue(now)
			if err != nil {
				return err
			}
			if due {
				servers := derive.MCPServers(procs, d.Sigs, now)
				if err := tx.ObserveMCP(now, servers.Samples); err != nil {
					return err
				}
			}
		}
		if p.Machine {
			sample, err := collected.sample, collected.machineErr
			if e := source("machine", err); e != nil {
				return e
			}
			if err != nil {
				if err := memoryPressureInbox(tx, model.MachineSample{}, now); err != nil {
					return err
				}
			}
			if err == nil {
				if sample.TS.IsZero() {
					sample.TS = now
				}
				if err := tx.InsertSample(sample); err != nil {
					return err
				}
				if err := lowDiskInbox(tx, d.Cfg.Machine, sample, now); err != nil {
					return err
				}
				if err := memoryPressureInbox(tx, sample, now, procs); err != nil {
					return err
				}
				sample.MachineID = tx.MachineID()
				if err := appendFeed(tx, feed.MachineSample, sample, share, now); err != nil {
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
				m.OS, m.Arch = platform.ProcessOS(d.Plat), runtime.GOARCH
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
			if procsOK && s.StartedFrom == "" && !s.Hosted && s.EndedAt == nil && s.Label != "dead" && s.Label != "ended" && !s.ProcStart.IsZero() {
				if proc, ok := work.prev[s.PID]; ok && derive.SameStart(proc.StartTime, s.ProcStart) {
					s.StartedFrom = derive.LaunchSurface(work.prev, s.PID)
				}
			}
			if s.Adapter == "agy" {
				if err := tx.SetMeta("warn:agy:unsupported_version", "process-level; title/state schema not yet supported"); err != nil {
					return err
				}
			}
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
					if err := appendFeed(tx, feed.Handoff, feed.HandoffRow{MachineID: tx.MachineID(), Session: feed.SessionKey{Adapter: s.Adapter, ExternalID: s.ExternalID}, TS: c.TS, Text: c.Text}, share, now); err != nil {
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
					if err := appendFeed(tx, feed.Handoff, feed.HandoffRow{MachineID: tx.MachineID(), Session: feed.SessionKey{Adapter: h.Adapter, ExternalID: h.SessionID}, TS: h.TS, Text: h.Message}, share, now); err != nil {
						return err
					}
				}
			}
		}
		if err := observeSessions(tx, work, ss, collected.logs, procs, previousProcs, procsOK, now); err != nil {
			return err
		}
		idx, err := derive.NewProcIndexForOS(procs, platform.ProcessOS(d.Plat))
		if err != nil {
			return err
		}
		// Inbox grouping needs parent IDs before labels consume the unseen items.
		parents := derive.LinkParents(ss, idx)
		if err := explicitParents(tx, ss, parents); err != nil {
			return err
		}
		if err := syncInbox(tx, d, ss, parents, hooks, baseline); err != nil {
			return err
		}
		if err := mergeAgents(tx, collected.agents, ss, now); err != nil {
			return err
		}
		if err := tx.CloseAttentionGrowth(now); err != nil {
			return err
		}
		if err := writeInboxJumps(tx, ss, collected.jumps); err != nil {
			return err
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
			if err := labelSession(tx, d, s, idx, events, unseen[s.ID], now); err != nil {
				return err
			}
		}
		parents = derive.LinkParents(ss, idx)
		if err := explicitParents(tx, ss, parents); err != nil {
			return err
		}
		for i := range ss {
			ss[i].ParentID = nil
			if id, ok := parents[ss[i].ID]; ok {
				parent := id
				ss[i].ParentID = &parent
			}
		}
		if p.Git || p.GitIdentity {
			if err := writeGit(tx, collected.git, collected.gitErr, func(name string, err error) error {
				if p.GitIdentity {
					name = "git_identity"
				}
				return source(name, err)
			}, &r); err != nil {
				return err
			}
			if p.GitFull && r.SourceErrors["git"] == "" {
				work.gitFullAt = now
			}
		}
		if err := mapRepos(tx, ss, collected.paths); err != nil {
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
			orphanOpts := []derive.OrphanOption{derive.WithSpawnMemory(memory), derive.WithClassifier(d.classifier)}
			if collected.subreaper > 1 {
				orphanOpts = append(orphanOpts, derive.WithSubreaper(collected.subreaper))
			}
			orphans := derive.FindOrphans(idx, ss, d.Sigs, now, d.Cfg.OrphanMinAge, prev, orphanOpts...)
			var peers map[int32]bool
			if _, ok := d.Plat.(platform.SocketProber); ok && p.Sockets && len(orphans) > 0 {
				peers = collected.peers
				if e := source("sockets", collected.socketErr); e != nil {
					return e
				}
			}
			orphans = derive.FindOrphans(idx, ss, d.Sigs, now, d.Cfg.OrphanMinAge, prev, append(orphanOpts, derive.WithExternalPeers(peers))...)
			if err := tx.ReplaceOrphans(orphans); err != nil {
				return err
			}
			r.Orphans = len(orphans)
			// work.procs is the table this run last committed (nil after a
			// restart), so only changed rows need writing.
			if err := tx.SyncProcs(work.procs, procs); err != nil {
				return err
			}
			work.procs = slices.Clone(procs)
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
		if feedEnabled {
			afterFeed, err := passFeedRows(tx, now, share)
			if err != nil {
				return err
			}
			if err := appendPassChanges(tx, beforeFeed, afterFeed, now); err != nil {
				return err
			}
		}
		if procsOK {
			keys := make(map[int64]feed.SessionKey, len(ss))
			for _, s := range ss {
				keys[s.ID] = feed.SessionKey{Adapter: s.Adapter, ExternalID: s.ExternalID}
			}
			if err := appendFeed(tx, feed.Procs, feed.ProcsRow{MachineID: tx.MachineID(), Processes: procs, Sessions: keys}, share, now); err != nil {
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
		state.procs = work.procs
		state.logs = work.logs
	}
	if err == nil {
		err = alertInbox(ctx, d, st)
	}
	return r, err
}

func sessionKey(a, id string) string { return a + "\x00" + id }

// Keep unresolved adapter references in transactional metadata so a parent
// first seen after a collector restart can still be linked. Session row IDs
// are resolved only through the local machine's store view.
func explicitParents(tx *store.Tx, ss []model.Session, parents map[int64]int64) error {
	stored, err := tx.Sessions(store.SessionFilter{IncludeEnded: true})
	if err != nil {
		return err
	}
	ids := make(map[string]int64, len(stored))
	for _, s := range stored {
		ids[sessionKey(s.Adapter, s.ExternalID)] = s.ID
	}
	for _, s := range ss {
		key := "session-parent:" + sessionKey(s.Adapter, s.ExternalID)
		ref := s.ParentExternalID
		if ref != "" {
			if err := tx.SetMeta(key, ref); err != nil {
				return err
			}
		} else {
			ref, err = tx.Meta(key)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
		}
		if ref == "" {
			continue
		}
		// Explicit evidence also suppresses an unrelated inferred parent while
		// the referenced row is still missing.
		delete(parents, s.ID)
		id := ids[sessionKey(s.Adapter, ref)]
		if id == 0 || id == s.ID {
			continue
		}
		seen := map[int64]bool{s.ID: true}
		cycle := false
		for p := id; p != 0; p = parents[p] {
			if seen[p] {
				cycle = true
				break
			}
			seen[p] = true
		}
		if !cycle {
			parents[s.ID] = id
		}
	}
	return nil
}

func collectFeedEnabled(d Deps, dir string) (bool, error) {
	enabled := d.Cfg.Fleet.Serve && !d.snapshot
	if !enabled && !d.snapshot {
		if dir == "" {
			dir = config.DataDir()
		}
		viewers, err := fleet.LoadViewers(filepath.Join(dir, "fleet"))
		if err != nil {
			return false, err
		}
		enabled = len(viewers) > 0
	}
	return enabled, nil
}

func configurePassFeed(d Deps, tx *store.Tx, enabled bool) (bool, error) {
	if err := tx.SetFeedShare(feed.EffectiveShare(feed.ShareText(d.Cfg.Fleet.ShareText), d.Cfg.CaptureText)); err != nil {
		return false, err
	}
	return enabled, tx.ConfigureFeed(enabled)
}

type spoolResult struct {
	events []model.HookEvent
	next   map[string]model.Offset
	err    error
}

func collectSpool(d Deps, stored map[string]model.Offset) spoolResult {
	offsets := make(map[string]model.Offset)
	prefix := spoolOffsetPrefix(d)
	for path, offset := range stored {
		if name, ok := strings.CutPrefix(path, prefix); ok && !strings.ContainsAny(name, "/\\") {
			offsets[name] = offset
		}
	}
	events, next, err := spool.Drain(d.SpoolDir, offsets, d.spoolSince)
	return spoolResult{events: events, next: next, err: err}
}

func writeSpool(d Deps, tx *store.Tx, batch spoolResult, now time.Time, source func(string, error) error) ([]model.HookEvent, error) {
	if err := source("spool", batch.err); err != nil {
		return nil, err
	}
	for name, o := range batch.next {
		if err := tx.SetOffset(spoolOffsetPrefix(d)+name, o); err != nil {
			return nil, err
		}
	}
	var hooks []model.HookEvent
	for _, ev := range batch.events {
		if ev.Adapter == "copilot" && !d.Cfg.Adapters["copilot"].Enabled {
			continue
		}
		if ev.Event == "summary" {
			if ev.Summary == nil || ev.MachineID != tx.MachineID() {
				continue
			}
			s := *ev.Summary
			if s.UsedSnippets && !d.Cfg.CaptureSnippets() {
				continue
			}
			s.Text = scrub.Snippet(s.Text)
			if err := tx.UpsertSummary(s); err != nil {
				return nil, err
			}
			continue
		}
		if ev.Event == "ack" {
			if ev.MachineID != "" && ev.MachineID != tx.MachineID() {
				if err := tx.ForMachine(ev.MachineID).MarkAckPending(ev.Message, now); err != nil {
					return nil, err
				}
				continue
			}
			if _, err := tx.AckInbox(ev.Message, ev.TS); err != nil {
				return nil, err
			}
			if err := tx.SetMeta("ack:"+ev.Message, now.Format(time.RFC3339Nano)); err != nil {
				return nil, err
			}
			continue
		}
		ev = prepareHook(d, ev)
		if ev.Adapter == "copilot" {
			if err := tx.SetMeta("hooks:copilot:received", "1"); err != nil {
				return nil, err
			}
		}

		event := model.Event{MachineID: tx.MachineID(), Adapter: ev.Adapter, ExternalID: ev.SessionID, TS: ev.TS, Kind: hookKind(ev), Summary: ev.Message}
		if ev.Adapter == "copilot" && ev.Tool != "" {
			event.Metadata = &model.EventMetadata{Tool: ev.Tool}
		}
		if err := tx.InsertEvent(event); err != nil {
			return nil, err
		}
		share := feed.EffectiveShare(feed.ShareText(d.Cfg.Fleet.ShareText), d.Cfg.CaptureText)
		if err := appendFeed(tx, feed.Event, event, share, now); err != nil {
			return nil, err
		}
		hooks = append(hooks, ev)
	}
	return hooks, nil
}

func hookKind(ev model.HookEvent) string {
	// Retain idle as protocol metadata, without manufacturing a permission wait.
	if ev.Event == "Notification" && ev.NotificationType == "idle_prompt" {
		return "Notification:idle_prompt"
	}
	return ev.Event
}

func prepareHook(d Deps, ev model.HookEvent) model.HookEvent {
	if d.Cfg.CaptureSnippets() {
		ev.Message = scrub.Snippet(ev.Message)
		ev.Detail = scrub.Snippet(ev.Detail)
	} else {
		ev.Message = ""
		ev.Detail = ""
		ev.Prompt = ""
	}
	if ev.Adapter == "copilot" && !d.Cfg.CaptureSnippets() {
		ev.Title = ""
		if ev.Event == "errorOccurred" {
			ev.Reason = ""
		}
	}
	ev.Title = scrub.Snippet(ev.Title)
	ev.Prompt = scrub.Snippet(ev.Prompt)
	ev.Tool = scrub.Snippet(ev.Tool)
	return ev
}

func appendFeed(tx *store.Tx, kind feed.Kind, row any, share feed.ShareText, now time.Time) error {
	key, payload := feed.Encode(kind, row, share)
	if payload == nil && kind == feed.Handoff && share != feed.Snippets {
		return nil
	}
	_, err := tx.AppendFeed(string(kind), key, payload, now)
	return err
}

type passFeedRow struct {
	kind       feed.Kind
	key        string
	row        any
	comparison any
	share      feed.ShareText
}

var evidenceDuration = regexp.MustCompile(`\b[0-9]+(?:\.[0-9]+)?(?:h|ms|m|s)(?:[0-9]+(?:\.[0-9]+)?(?:h|ms|m|s))*\b`)

// passFeedRows reads mutable entities on the pass transaction. Stored session
// changes still emit a row when the changed field is withheld by text policy.
// Append-only events, handoffs, samples and bulk procs are emitted at insertion.
func passFeedRows(tx *store.Tx, now time.Time, share feed.ShareText) (map[string]passFeedRow, error) {
	rows := make(map[string]passFeedRow)
	add := func(kind feed.Kind, row any) {
		var key string
		comparisonRow := row
		switch v := row.(type) {
		case feed.SessionRow:
			key = feed.Key(v.Adapter, v.ExternalID)
			// Evidence is an observation at the feed row's timestamp. Keep its
			// readable sampled age, but age alone must not publish every wake.
			v.Evidence = evidenceDuration.ReplaceAllString(v.Evidence, "<age>")
			if v.Mixer != nil {
				mixed := *v.Mixer
				mixed.ObservedAt = time.Time{}
				mixed.Evidence = evidenceDuration.ReplaceAllString(mixed.Evidence, "<age>")
				if mixed.Input != nil {
					input := *mixed.Input
					input.LastAPIError = evidenceDuration.ReplaceAllString(input.LastAPIError, "<age>")
					mixed.Input = &input
				}
				mixed.Rows = append([]model.StateSignal(nil), mixed.Rows...)
				for i := range mixed.Rows {
					mixed.Rows[i].Age = 0
					mixed.Rows[i].Strength = 0
				}
				v.Mixer = &mixed
			}
			comparisonRow = v
		case model.SourceStatus:
			key = v.Name
			failed := !v.LastErrorAt.IsZero() && !v.LastErrorAt.Before(v.LastOKAt)
			v.LastOKAt, v.LastErrorAt = time.Time{}, time.Time{}
			comparisonRow = struct {
				Source model.SourceStatus
				Failed bool
			}{v, failed}
		case feed.WorktreeRow:
			key = v.Path
			// Git refreshes UpdatedAt on every scan; only publish changed facts.
			v.UpdatedAt = time.Time{}
			comparisonRow = v
		case model.Repo:
			key = v.Path
		case feed.FilesRow:
			key = feed.Key(v.Session.Adapter, v.Session.ExternalID)
		case feed.SummaryRow:
			if share != feed.Titles && share != feed.Snippets || v.UsedSnippets && share != feed.Snippets {
				return
			}
			key = feed.Key(v.RepoPath, v.UsedSnippets)
		case feed.InboxRow:
			key = v.ID
		case model.Orphan:
			key = v.ID
		case model.Machine:
			key = v.MachineID
			if key == "" {
				key = v.ID
			}
			// Compare precisely the host metadata exported by feed.Encode;
			// LastSeen and the viewer's fleet state are collection cadence.
			comparisonRow = model.Machine{MachineID: key, Hostname: v.Hostname, OS: v.OS, Arch: v.Arch, Cores: v.Cores, MemTotal: v.MemTotal, DrigaVersion: v.DrigaVersion}
		}
		rows[feed.Key(kind, key)] = passFeedRow{kind: kind, key: key, row: row, comparison: comparisonRow, share: share}
	}
	repos, err := tx.Repos()
	if err != nil {
		return nil, err
	}
	repoPaths := make(map[int64]string)
	for _, r := range repos {
		repoPaths[r.ID] = r.Path
		clean := feed.StripURLCredentials(r.RemoteURL)
		if clean != r.RemoteURL {
			r.RemoteURL = clean
			if _, err := tx.UpsertRepo(r); err != nil {
				return nil, err
			}
		}
		add(feed.Repo, r)
	}
	worktrees, err := tx.Worktrees(0)
	if err != nil {
		return nil, err
	}
	worktreePaths := make(map[int64]string)
	for _, w := range worktrees {
		worktreePaths[w.ID] = w.Path
		add(feed.Worktree, feed.WorktreeRow{Worktree: w, RepoPath: repoPaths[w.RepoID]})
	}
	ss, err := tx.Sessions(store.SessionFilter{IncludeEnded: true})
	if err != nil {
		return nil, err
	}
	keys := make(map[int64]feed.SessionKey, len(ss))
	for _, s := range ss {
		keys[s.ID] = feed.SessionKey{Adapter: s.Adapter, ExternalID: s.ExternalID}
	}
	for _, s := range ss {
		row := feed.SessionRow{Session: s}
		if s.ParentID != nil {
			if parent, ok := keys[*s.ParentID]; ok {
				row.Parent = &parent
			}
		}
		if s.RepoID != nil {
			row.RepoPath = repoPaths[*s.RepoID]
		}
		if s.WorktreeID != nil {
			row.WorktreePath = worktreePaths[*s.WorktreeID]
		}
		add(feed.Session, row)
	}
	files, err := tx.SessionFiles(now.Add(-30 * time.Minute))
	if err != nil {
		return nil, err
	}
	for id, f := range files {
		if key, ok := keys[id]; ok {
			add(feed.SessionFiles, feed.FilesRow{MachineID: tx.MachineID(), Session: key, Files: f})
		}
	}
	items, err := tx.Inbox(time.Time{}, true)
	if err != nil {
		return nil, err
	}
	summaries, err := tx.Summaries(0)
	if err != nil {
		return nil, err
	}
	for _, s := range summaries {
		add(feed.Summary, feed.SummaryRow{Summary: s, RepoPath: repoPaths[s.ProjectID]})
	}
	for _, i := range items {
		// Cleanup's machine-level attention stays local. The fleet feed
		// requires session keys; never publish an empty session reference.
		if (i.Kind == "low_disk" || i.Kind == "memory_pressure") && i.SessionID == 0 {
			continue
		}
		add(feed.Inbox, feed.InboxRow{InboxItem: i, Session: keys[i.SessionID]})
	}
	orphans, err := tx.Orphans()
	if err != nil {
		return nil, err
	}
	for _, o := range orphans {
		add(feed.Orphan, o)
	}
	sources, err := tx.Sources()
	if err != nil {
		return nil, err
	}
	for _, s := range sources {
		add(feed.Source, s)
	}
	m, err := tx.Machine()
	if err == nil {
		add(feed.Machine, m)
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	return rows, nil
}

func appendPassChanges(tx *store.Tx, before, after map[string]passFeedRow, now time.Time) error {
	// Stable order also keeps repositories ahead of their referencing checkouts.
	order := map[feed.Kind]int{feed.Machine: 0, feed.Repo: 1, feed.Worktree: 2, feed.Session: 3, feed.SessionFiles: 4, feed.Inbox: 5, feed.Orphan: 6, feed.Source: 7, feed.Summary: 8}
	var changed []passFeedRow
	for id, row := range after {
		previous, exists := before[id]
		publish := !exists
		if exists && !reflect.DeepEqual(previous.comparison, row.comparison) {
			// Most rows are unchanged. Only use wire comparison for real field
			// differences, preserving JSON omission/normalization semantics.
			old, _ := jsonx.Marshal(previous.comparison)
			current, _ := jsonx.Marshal(row.comparison)
			publish = !bytes.Equal(old, current)
		}
		if row.kind == feed.Source && !publish && !reflect.DeepEqual(previous.row, row.row) {
			last, err := tx.Meta("feed_source:" + row.key)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			at, _ := time.Parse(time.RFC3339Nano, last)
			publish = now.Sub(at) >= 5*time.Minute
		}
		if publish {
			changed = append(changed, row)
		}
	}
	sort.Slice(changed, func(i, j int) bool {
		if order[changed[i].kind] != order[changed[j].kind] {
			return order[changed[i].kind] < order[changed[j].kind]
		}
		return changed[i].key < changed[j].key
	})
	for _, row := range changed {
		if err := appendFeed(tx, row.kind, row.row, row.share, now); err != nil {
			return err
		}
		if row.kind == feed.Source {
			if err := tx.SetMeta("feed_source:"+row.key, now.Format(time.RFC3339Nano)); err != nil {
				return err
			}
		}
	}
	var removed []string
	for id := range before {
		if _, ok := after[id]; !ok {
			removed = append(removed, id)
		}
	}
	sort.Strings(removed)
	for _, id := range removed {
		row := before[id]
		if err := appendFeed(tx, feed.Delete, feed.DeleteRow{Kind: row.kind, Key: row.key}, feed.None, now); err != nil {
			return err
		}
	}
	return nil
}

func mergeObs(dst *model.SessionObs, src model.SessionObs) {
	dst.Adapter, dst.ExternalID = src.Adapter, src.ExternalID
	if src.ParentExternalID != "" {
		dst.ParentExternalID = src.ParentExternalID
	}
	// Preserve the historical Unix canonical identity for local symlinks.
	// Windows observations and foreign paths retain their recorded spelling;
	// comparisons/discovery normalize separate keys, not the stored observation.
	if src.Cwd != "" && runtime.GOOS != "windows" && !pathx.WindowsStyle(src.Cwd) {
		src.Cwd = gitscan.NormalizePath(src.Cwd)
	}
	if len(src.FilesTouched) > 0 {
		files := make([]model.FileTouch, len(src.FilesTouched))
		for i, f := range src.FilesTouched {
			if runtime.GOOS != "windows" && !pathx.WindowsStyle(f.Path) {
				f.Path = gitscan.NormalizePath(f.Path)
			}
			files[i] = f
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
		dst.RegistryUpdatedAt = src.RegistryUpdatedAt
		dst.Hosted = false
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
	if src.Adapter == "gemini" {
		// Gemini observations are complete folds, including rewinds and
		// recovery snapshots. Timestamps and titles can move backwards.
		dst.Title, dst.LastPrompt = src.Title, src.LastPrompt
		dst.LastActivityAt, dst.LastPromptAt = src.LastActivityAt, src.LastPromptAt
		dst.TurnOpen = src.TurnOpen
		dst.Completions, dst.FilesTouched = src.Completions, src.FilesTouched
	}
}

func linkCodex(ss []model.Session, procs []model.Proc, plat platform.Platform) {
	used := make(map[int32]bool)
	var order []int
	for i, s := range ss {
		if s.PID != 0 {
			used[s.PID] = true
		}
		if (s.Adapter == "codex" || s.Adapter == "gemini" && s.ParentExternalID == "") && s.PID == 0 && !s.Hosted && s.Cwd != "" && !s.StartedAt.IsZero() {
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
	// Liveness and resolved cwd belong to the process, not the pair: check each
	// once per pass. Hundreds of ended sessions times thousands of processes
	// otherwise cost a syscall and a symlink walk per pair.
	alive := make(map[int]bool)
	cwds := make(map[int]string)
	live := func(j int) bool {
		v, ok := alive[j]
		if !ok {
			v = plat.Alive(procs[j].PID, procs[j].StartTime)
			alive[j] = v
		}
		return v
	}
	cwd := func(j int) string {
		v, ok := cwds[j]
		if !ok {
			v = gitscan.NormalizePath(procs[j].Cwd)
			cwds[j] = v
		}
		return v
	}
	for _, i := range order {
		s := &ss[i]
		var best *model.Proc
		sessionCwd := ""
		for j := range procs {
			p := &procs[j]
			if used[p.PID] || p.Agent != s.Adapter || (p.Role != "interactive" && p.Role != "headless") || p.Cwd == "" || p.StartTime.IsZero() || p.StartTime.After(s.StartedAt.Add(5*time.Second)) || !live(j) {
				continue
			}
			if sessionCwd == "" {
				sessionCwd = gitscan.NormalizePath(s.Cwd)
			}
			if !pathx.RecordedEqual(cwd(j), sessionCwd) {
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

func mapRepos(tx *store.Tx, ss []model.Session, paths map[string]string) error {
	ws, err := tx.Worktrees(0)
	if err != nil {
		return err
	}
	normalize := func(path string) string { return paths[path] }
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
			if len(path) > best && pathx.RecordedWithin(cwd, path) {
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

// spoolOffsetPrefix keeps platform-default and explicitly overridden spool offsets separate.
func spoolOffsetPrefix(d Deps) string {
	if os.Getenv("DRIGA_SPOOL_DIR") == "" && filepath.Clean(d.SpoolDir) == filepath.Clean(config.SpoolDir()) {
		return "spool/state/"
	}
	return "spool/"
}
