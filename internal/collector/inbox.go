package collector

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/labstack/driga/internal/adapter/copilot"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/fleet"
	attention "github.com/labstack/driga/internal/inbox"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func hookSessions(ss []model.Session, hooks []model.HookEvent) []model.Session {
	seen := map[string]int{}
	for i, s := range ss {
		seen[sessionKey(s.Adapter, s.ExternalID)] = i
	}
	for _, h := range hooks {
		if h.Adapter != "claude" && h.Adapter != "codex" && h.Adapter != "copilot" || h.SessionID == "" || h.Event == "notify" {
			continue
		}
		key := sessionKey(h.Adapter, h.SessionID)
		i, ok := seen[key]
		if !ok {
			i = len(ss)
			seen[key] = i
			ss = append(ss, model.Session{SessionObs: model.SessionObs{Adapter: h.Adapter, ExternalID: h.SessionID, Cwd: h.Cwd, Origin: "hook", StartedAt: h.TS, Hosted: h.Adapter != "copilot"}})
		}
		if h.Adapter == "copilot" {
			copilot.Fold(&ss[i].SessionObs, h)
		}
	}
	return ss
}

func syncInbox(tx *store.Tx, d Deps, ss []model.Session, parents map[int64]int64, hooks []model.HookEvent, baseline time.Time) error {
	type change struct {
		at       time.Time
		item     *model.InboxItem
		session  int64
		explicit bool
	}
	var changes []change
	copilotActivity := func(event string, at time.Time, id int64) {
		switch event {
		case "postToolUse", "postToolUseFailure", "agentStop", "errorOccurred", "sessionEnd":
			changes = append(changes, change{at: at, session: id})
		case "userPromptSubmitted":
			changes = append(changes, change{at: at, session: id, explicit: true})
		}
	}
	for _, v := range derive.InboxItems(ss, parents, hooks, baseline) {
		i := attention.Policy(v, d.Cfg.CaptureText)
		changes = append(changes, change{at: i.TS, item: &i})
	}
	ids := map[string]int64{}
	for _, s := range ss {
		ids[sessionKey(s.Adapter, s.ExternalID)] = s.ID
		if !s.LastPromptAt.IsZero() {
			changes = append(changes, change{at: s.LastPromptAt, session: s.ID, explicit: true})
		}
		// Adapter timestamps are scoped to this session, never folded from workers.
		if s.Adapter != "copilot" && !s.LastActivityAt.IsZero() {
			changes = append(changes, change{at: s.LastActivityAt, session: s.ID})
		}
	}
	for _, h := range hooks {
		id := ids[sessionKey(h.Adapter, h.SessionID)]
		if h.Adapter == "copilot" {
			copilotActivity(h.Event, h.TS, id)
			if copilot.Wait(h) {
				// Hook processes can finish out of order. Reapply later stored
				// activity for this session so a delayed wait cannot reopen it.
				events, err := tx.Events("copilot", h.SessionID, h.TS)
				if err != nil {
					return err
				}
				for _, e := range events {
					copilotActivity(e.Kind, e.TS, id)
				}
			}
			continue
		}
		switch h.Event {
		case "Stop":
			changes = append(changes, change{at: h.TS, session: id, explicit: true})
		case "PostToolUse":
			changes = append(changes, change{at: h.TS, session: id})
		case "UserPromptSubmit":
			changes = append(changes, change{at: h.TS, session: id, explicit: true})
		}
	}
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].at.Before(changes[j].at) })
	for _, c := range changes {
		if c.item != nil {
			if err := tx.UpsertInboxItem(*c.item); err != nil {
				return err
			}
			continue
		}
		if c.session == 0 {
			continue
		}
		var err error
		if c.explicit {
			err = tx.CloseAttentionBefore(c.session, c.at)
		} else {
			err = tx.CloseAttentionActivity(c.session, c.at)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func resolveInboxJumps(ctx context.Context, tx *store.Tx, d Deps, ss []model.Session, processes []model.Proc) error {
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	// Resolve from stored process identities after the integration reads, never
	// from a guessed pane name or a parent process's activity.
	rows, err := tx.Inbox(time.Time{}, false)
	if err != nil {
		return err
	}
	procs := map[int32]model.Proc{}
	if processes == nil && len(rows) > 0 {
		processes, err = tx.Procs()
		if err != nil {
			return err
		}
	}
	for _, p := range processes {
		procs[p.PID] = p
	}
	var panes []attention.Pane
	if len(rows) > 0 {
		panes = attention.ReadPanes(ctx, d.InboxRunner)
	}
	byID := map[int64]model.Session{}
	for _, s := range ss {
		byID[s.ID] = s
	}
	for _, i := range rows {
		s, ok := byID[i.SessionID]
		if !ok {
			continue
		}
		p := procs[s.PID]
		tty := ""
		if s.ProcStart.IsZero() || derive.SameStart(p.StartTime, s.ProcStart) {
			tty = p.TTY
		}
		j := attention.ResolveJump(s, tty, panes)
		if d.Cfg.Inbox.JumpTerminal && runtime.GOOS == "darwin" && (j == nil || j.Kind != "tmux") {
			if terminal := attention.TerminalJump(ctx, d.InboxRunner, tty); terminal != nil {
				terminal.Cwd = s.Cwd
				j = terminal
			}
		}
		if err := tx.SetInboxJump(i.ID, j); err != nil {
			return err
		}
	}
	return nil
}

// mergeAgents remembers each blocked generation transactionally, so an ack is
// not undone by the next identical poll, including after collector restart.
func mergeAgents(tx *store.Tx, agents []attention.Agent, ss []model.Session, now time.Time) error {
	ids := map[string]int64{}
	for _, s := range ss {
		if s.Adapter == "claude" {
			ids[s.ExternalID] = s.ID
		}
	}
	for _, a := range agents {
		key := "agents_blocked:" + a.ID
		if a.State != "blocked" {
			if err := tx.SetMeta(key, ""); err != nil {
				return err
			}
			continue
		}
		raw, err := tx.Meta(key)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		at, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			at = now
			if err := tx.SetMeta(key, at.Format(time.RFC3339Nano)); err != nil {
				return err
			}
		}
		id := ids[a.ID]
		if id == 0 {
			continue
		}
		i := model.InboxItem{ID: fmt.Sprintf("claude:%s:%d", a.ID, at.UnixNano()), SessionID: id, Kind: "waiting", TS: at, WaitingSinceAt: at, Reason: a.Reason, Source: "agents_cli", Summary: "waiting for input"}
		if err := tx.UpsertInboxItem(i); err != nil {
			return err
		}
	}
	return nil
}

// alertInbox runs only in the collector/viewer. Paired data is already bounded
// by the sender's share policy; local capture can restrict it further.
func alertInbox(ctx context.Context, d Deps, st *store.Store) error {
	if d.Notifier == nil || d.snapshot {
		return nil
	}
	scope := store.Scope{Machines: []string{st.MachineID()}}
	// Pairing is proved by the viewer's trust file, not by cached rows alone.
	peers, _ := fleet.LoadPeers(filepath.Join(config.DataDir(), "fleet"))
	for _, p := range peers {
		if p.MachineID != "" {
			scope.Machines = append(scope.Machines, p.MachineID)
		}
	}
	var notes []attention.Notification
	err := st.Tx(ctx, func(tx *store.Tx) error {
		rows, err := tx.InboxAlertCandidates(scope)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		seen, last := map[string]time.Time{}, map[string]time.Time{}
		sessions, err := tx.InboxSessionLabels(scope)
		if err != nil {
			return err
		}
		// Only candidate keys are loaded. Range-reading seen/repeat metadata
		// would allocate for the entire consumed backlog again.
		load := func(prefix, key string, dst map[string]time.Time) error {
			if _, ok := dst[key]; ok {
				return nil
			}
			raw, err := tx.Meta(prefix + key)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			if at, err := time.Parse(time.RFC3339Nano, raw); err == nil {
				dst[key] = at
			}
			return nil
		}
		for _, i := range rows {
			if err := load("inbox_alert:", i.MachineID+":"+i.ID, seen); err != nil {
				return err
			}
			if err := load("inbox_alert_session:", attention.AlertRepeatKey(i), last); err != nil {
				return err
			}
		}
		previousSeen, previousLast := maps.Clone(seen), maps.Clone(last)
		notes = attention.PlanAlerts(rows, sessions, d.Cfg.Inbox, d.Cfg.CaptureText, d.now(), seen, last)
		// Remote IDs can collide with local IDs and cannot be jumped to here.
		for k := range notes {
			if notes[k].MachineID != st.MachineID() {
				notes[k].ItemID = ""
			}
		}
		for _, i := range rows {
			if _, consumed := seen[i.MachineID+":"+i.ID]; consumed {
				if err := tx.MarkInboxAlerted(i.MachineID, i.ID); err != nil {
					return err
				}
			}
		}
		for k, v := range seen {
			if previousSeen[k].Equal(v) {
				continue
			}
			if err := tx.SetMeta("inbox_alert:"+k, v.Format(time.RFC3339Nano)); err != nil {
				return err
			}
		}
		for k, v := range last {
			if previousLast[k].Equal(v) {
				continue
			}
			if err := tx.SetMeta("inbox_alert_session:"+k, v.Format(time.RFC3339Nano)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, n := range notes {
		_ = d.Notifier.Notify(ctx, n)
	}
	return nil
}
