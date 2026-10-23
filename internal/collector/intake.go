package collector

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/intake"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/scrub"
	"github.com/labstack/driga/internal/store"
)

type intakeResult struct {
	unmatched int
	err       error
}
type intakeRequest struct {
	batch   intake.Batch
	reply   chan intakeResult
	failure error
	stats   func() intake.Stats
}

func startIntake(ctx context.Context, d Deps, st *store.Store, requests chan<- intakeRequest) (func(), error) {
	if !d.Cfg.Intake.OTLP {
		return nil, nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	// Every writer remains on Run's goroutine, including startup diagnostics.
	err := st.Tx(ctx, func(tx *store.Tx) error {
		keys := []string{"received", "accepted", "records_rejected", "metrics_rejected", "dropped", "unmatched", "ignored", "invalid_records", "rejected", "parse_failures"}
		for _, reason := range intake.RejectionReasons() {
			keys = append(keys, "reject:"+reason)
		}
		for _, key := range keys {
			if err := tx.SetMeta("otlp:"+key, "0"); err != nil {
				return err
			}
		}
		return tx.SetMeta("otlp:listen", "")
	})
	if err != nil {
		cancel()
		return nil, err
	}
	ln, err := intake.Listen(d.Cfg.Intake.Listen)
	if err != nil {
		cancel()
		return nil, recordIntakeError(ctx, d, st, err)
	}
	if err := st.Tx(ctx, func(tx *store.Tx) error {
		if err := tx.SetSourcePending("otlp"); err != nil {
			return err
		}
		return tx.SetMeta("otlp:listen", ln.Addr().String())
	}); err != nil {
		ln.Close()
		cancel()
		return nil, err
	}
	var handler *intake.Handler
	stats := func() intake.Stats { return handler.Stats() }
	handler = intake.New(func(reqCtx context.Context, b intake.Batch) (int, error) {
		req := intakeRequest{batch: b, reply: make(chan intakeResult, 1), stats: stats}
		select {
		case requests <- req:
		case <-runCtx.Done():
			return 0, intake.ErrBusy
		case <-reqCtx.Done():
			return 0, intake.ErrBusy
		default:
			return 0, intake.ErrBusy
		}
		select {
		case result := <-req.reply:
			return result.unmatched, result.err
		case <-runCtx.Done():
			return 0, intake.ErrBusy
		case <-reqCtx.Done():
			return 0, intake.ErrBusy
		}
	}, func() {
		// Coalesce notifications under load. Every queued notification/batch
		// reads the latest cumulative snapshot, so a full queue loses no counts.
		select {
		case requests <- intakeRequest{stats: stats}:
		default:
		}
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := intake.Serve(runCtx, ln, handler); err != nil {
			select {
			case requests <- intakeRequest{failure: err}:
			case <-runCtx.Done():
			}
		}
	}()
	return func() { cancel(); <-done }, nil
}

func recordIntakeStats(ctx context.Context, st *store.Store, s intake.Stats) error {
	return st.Tx(ctx, func(tx *store.Tx) error {
		counts := map[string]int64{"rejected": s.Rejected, "parse_failures": s.ParseFailures}
		for _, reason := range intake.RejectionReasons() {
			counts["reject:"+reason] = s.Reasons[reason]
		}
		for key, count := range counts {
			if err := tx.SetMeta("otlp:"+key, strconv.FormatInt(count, 10)); err != nil {
				return err
			}
		}
		return nil
	})
}

func recordIntakeError(ctx context.Context, d Deps, st *store.Store, cause error) error {
	return st.Tx(ctx, func(tx *store.Tx) error {
		now := d.now()
		if err := tx.SetSourceError("otlp", scrub.Snippet(cause.Error()), now); err != nil {
			return err
		}
		if err := tx.SetMeta("otlp:listen", ""); err != nil {
			return err
		}
		sources, err := tx.Sources()
		if err != nil {
			return err
		}
		for _, s := range sources {
			if s.Name == "otlp" {
				return appendFeed(tx, feed.Source, s, feed.None, now)
			}
		}
		return nil
	})
}

// labelSession is shared by full collection, spool wakes, and optional intake.
// All paths use the same stored events, process identity and derive table.
func labelSession(tx *store.Tx, d Deps, s *model.Session, idx derive.ProcIndex, events []model.Event, unseen bool, now time.Time) error {
	in := derive.StateInput{Obs: s.SessionObs, LastUserPrompt: s.LastPromptAt, Unseen: unseen}
	if p, ok := idx.Get(s.PID); ok && (s.ProcStart.IsZero() || derive.SameStart(p.StartTime, s.ProcStart)) {
		in.Proc = &p
	}
	if err := intakeInput(tx, *s, events, &in, now); err != nil {
		return err
	}
	mixSession(s, in, d.Cfg.StalledAfter, now)
	if s.Label == "ended" {
		return tx.CloseAttentionBefore(s.ID, *s.EndedAt)
	}
	return nil
}

// mixSession folds lifecycle evidence independently of persistence. Unseen
// completion display does not change whether a session is live for orphans.
func mixSession(s *model.Session, in derive.StateInput, stalledAfter time.Duration, now time.Time) {
	mixed := derive.LabelWithEvidence(in, now, derive.Thresholds{StalledAfter: stalledAfter})
	s.Mixer = &mixed
	s.Label, s.Heuristic, s.Evidence = mixed.Verdict, mixed.Heuristic, mixed.Evidence
	s.LastPromptAt = in.LastUserPrompt
	if s.Label == "ended" {
		s.TurnOpen = false
		if s.EndedAt == nil {
			at := now
			if in.LastLifecycle == "SessionEnd" {
				at = in.LastLifecycleAt
			}
			if s.RegistryStatus == "ended" && !s.RegistryUpdatedAt.IsZero() {
				at = s.RegistryUpdatedAt
			}
			s.EndedAt = &at
		}
		// Ending a session closes pending questions, never unread completions.
		return
	}
	if s.EndedAt != nil && in.LastLifecycleAt.After(*s.EndedAt) && in.LastLifecycle != "SessionEnd" && (in.Proc != nil || in.Obs.Hosted) {
		s.EndedAt = nil
	}
}

type intakeReader interface {
	IntakeState(int64) (store.IntakeState, error)
}

func intakeInput(tx intakeReader, s model.Session, events []model.Event, in *derive.StateInput, now time.Time) error {
	latest := func(dst **time.Time, ts time.Time) {
		if *dst == nil || ts.After(**dst) {
			at := ts
			*dst = &at
		}
	}
	for _, e := range events {
		if !strings.HasPrefix(e.Kind, "otlp:") && e.TS.After(in.LastHookAt) {
			in.LastHook, in.LastHookAt = e.Kind, e.TS
		}
		switch e.Kind {
		case "SessionStart", "SessionEnd", "UserPromptSubmit", "Stop", "Notification:idle_prompt":
			if e.TS.After(in.LastLifecycleAt) {
				in.LastLifecycle, in.LastLifecycleAt = e.Kind, e.TS
			}
		}
		switch e.Kind {
		case "Notification", "PermissionRequest":
			latest(&in.LastNotification, e.TS)
		case "Stop":
			latest(&in.LastStop, e.TS)
		case "UserPromptSubmit":
			if e.TS.After(in.LastUserPrompt) {
				in.LastUserPrompt = e.TS
			}
		case "PostToolUse":
			latest(&in.LastToolResult, e.TS)
		}
	}
	if s.Adapter != "claude" || s.ID == 0 {
		return nil
	}
	state, err := tx.IntakeState(s.ID)
	if err != nil {
		return err
	}
	if !state.LastDecision.IsZero() {
		in.LastDecision = &state.LastDecision
	}
	if !state.LastAPIRequest.IsZero() {
		in.LastAPIRequest = &state.LastAPIRequest
	}
	in.LastAPIError = state.LastAPIError
	var errorAt time.Time
	for _, e := range events {
		switch e.Kind {
		case "otlp:tool_result", "otlp:assistant_response", "otlp:user_prompt":
			latest(&in.LastToolResult, e.TS)
		case "otlp:api_error":
			if e.TS.After(errorAt) {
				errorAt = e.TS
				in.LastAPIError = e.Summary + fmt.Sprintf(" (%s ago)", max(time.Duration(0), now.Sub(e.TS)).Round(time.Second))
			}
		}
	}
	return nil
}

func applyIntake(ctx context.Context, d Deps, st *store.Store, b intake.Batch) (int, error) {
	unmatched := 0
	err := st.Tx(ctx, func(tx *store.Tx) error {
		now := d.now()
		share := feed.EffectiveShare(feed.ShareText(d.Cfg.Fleet.ShareText), d.Cfg.CaptureText)
		enabled, err := tx.Meta("feed_enabled")
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		var before map[string]passFeedRow
		// Match AppendFeed's default for an unconfigured store, but avoid
		// snapshotting the database when publication has been suspended.
		if enabled != "false" {
			before, err = passFeedRows(tx, now, share)
			if err != nil {
				return err
			}
		}
		var ss []model.Session
		byID := make(map[string]int)
		for _, f := range b.Facts {
			if _, ok := byID[f.SessionID]; ok {
				continue
			}
			s, err := tx.SessionByKey("claude", f.SessionID)
			if errors.Is(err, store.ErrNotFound) {
				byID[f.SessionID] = -1
				continue
			}
			if err != nil {
				return err
			}
			byID[f.SessionID] = len(ss)
			ss = append(ss, s)
		}
		changed := make(map[int]bool)
		for _, f := range b.Facts {
			i, ok := byID[f.SessionID]
			if !ok || i < 0 {
				unmatched++
				continue
			}
			s := &ss[i]
			state, err := tx.IntakeState(s.ID)
			if err != nil {
				return err
			}
			switch f.Name {
			case "tool_decision":
				switch f.Source {
				case "user_permanent", "user_temporary", "user_abort", "user_reject":
					if f.TS.After(state.LastDecision) {
						state.LastDecision = f.TS
					}
					if err := tx.CloseAttentionBefore(s.ID, f.TS); err != nil {
						return err
					}
				}
			case "tool.blocked_on_user":
				if f.TS.After(state.LastDecision) {
					state.LastDecision = f.TS
				}
				if err := tx.CloseAttentionBefore(s.ID, f.TS); err != nil {
					return err
				}
			case "api_request":
				if f.TS.After(state.LastAPIRequest) {
					state.LastAPIRequest = f.TS
				}
			case "api_error":
				// Read the newest stored error to prevent delayed exports regressing it.
				previous, err := tx.Events("claude", s.ExternalID, f.TS)
				if err != nil {
					return err
				}
				newer := false
				for _, e := range previous {
					if e.Kind == "otlp:api_error" {
						newer = true
						break
					}
				}
				if !newer {
					state.LastAPIError = f.Summary
				}
			case "user_prompt":
				if f.TS.After(s.LastPromptAt) {
					s.LastPromptAt = f.TS
				}
				if !f.TS.Before(s.LastActivityAt) {
					s.TurnOpen = true
				}
				if err := tx.CloseAttentionBefore(s.ID, f.TS); err != nil {
					return err
				}
			case "tool_result", "assistant_response":
				if err := tx.CloseAttentionActivity(s.ID, f.TS); err != nil {
					return err
				}
			}
			if err := tx.SetIntakeState(s.ID, state); err != nil {
				return err
			}
			event := model.Event{MachineID: tx.MachineID(), Adapter: "claude", ExternalID: s.ExternalID, TS: f.TS, Kind: "otlp:" + f.Name, Metadata: f.Metadata, Summary: scrub.Snippet(f.Summary)}
			if err := tx.InsertEvent(event); err != nil {
				return err
			}
			if err := appendFeed(tx, feed.Event, event, share, now); err != nil {
				return err
			}
			changed[i] = true
		}
		procs, err := tx.Procs()
		if err != nil {
			return err
		}
		idx, err := derive.NewProcIndexForOS(procs, platform.ProcessOS(d.Plat))
		if err != nil {
			return err
		}
		items, err := tx.Inbox(time.Time{}, false)
		if err != nil {
			return err
		}
		unseen := make(map[int64]bool)
		for _, it := range items {
			if it.Kind == "finished" {
				unseen[it.SessionID] = true
			}
		}
		for i := range changed {
			s := &ss[i]
			events, err := tx.Events(s.Adapter, s.ExternalID, time.Time{})
			if err != nil {
				return err
			}
			if err := labelSession(tx, d, s, idx, events, unseen[s.ID], now); err != nil {
				return err
			}
			// Fold activity after labeling so the optional fact can support idle registries.
			in := s.Mixer.Input
			if in.LastToolResult != nil && in.LastToolResult.After(s.LastActivityAt) {
				s.LastActivityAt = *in.LastToolResult
			}

			if _, err := tx.UpsertSession(*s); err != nil {
				return err
			}
		}
		for key, n := range map[string]int{
			"received":         len(b.Facts) + b.Rejected + b.Ignored + b.MetricsRejected,
			"accepted":         len(b.Facts) - unmatched,
			"records_rejected": b.Rejected + unmatched + b.MetricsRejected,
			"metrics_rejected": b.MetricsRejected,
			"dropped":          b.Dropped, "unmatched": unmatched, "ignored": b.Ignored, "invalid_records": b.Rejected,
		} {
			raw, err := tx.Meta("otlp:" + key)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			v, _ := strconv.Atoi(raw)
			if err := tx.SetMeta("otlp:"+key, strconv.Itoa(v+n)); err != nil {
				return err
			}
		}
		if len(b.Facts)+b.Rejected+b.Ignored+b.MetricsRejected > 0 {
			if err := tx.SetSourceOK("otlp", now); err != nil {
				return err
			}
		}
		if before == nil {
			return nil
		}
		after, err := passFeedRows(tx, now, share)
		if err != nil {
			return err
		}
		return appendPassChanges(tx, before, after, now)
	})
	return unmatched, err
}
