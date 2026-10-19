package collector

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/intake"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/scrub"
	"github.com/labstack/driga/internal/store"
)

type intakeResult struct {
	rejected int
	err      error
}
type intakeRequest struct {
	batch   intake.Batch
	reply   chan intakeResult
	failure error
}

func startIntake(ctx context.Context, d Deps, st *store.Store, requests chan<- intakeRequest) (func(), error) {
	if !d.Cfg.Intake.OTLP {
		return nil, nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	// Every writer remains on Run's goroutine, including startup diagnostics.
	err := st.Tx(ctx, func(tx *store.Tx) error {
		for _, key := range []string{"received", "dropped", "unmatched"} {
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
	handler := intake.New(func(reqCtx context.Context, b intake.Batch) (int, error) {
		req := intakeRequest{batch: b, reply: make(chan intakeResult, 1)}
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
			return result.rejected, result.err
		case <-runCtx.Done():
			return 0, intake.ErrBusy
		case <-reqCtx.Done():
			return 0, intake.ErrBusy
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

func intakeInput(tx *store.Tx, s model.Session, events []model.Event, in *derive.StateInput, now time.Time) error {
	if s.Adapter != "claude" {
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
		case "Notification", "PermissionRequest":
			if in.LastNotification == nil || e.TS.After(*in.LastNotification) {
				ts := e.TS
				in.LastNotification = &ts
			}
		case "Stop":
			if in.LastStop == nil || e.TS.After(*in.LastStop) {
				ts := e.TS
				in.LastStop = &ts
			}
		case "otlp:tool_result", "otlp:assistant_response", "otlp:user_prompt":
			if in.LastToolResult == nil || e.TS.After(*in.LastToolResult) {
				ts := e.TS
				in.LastToolResult = &ts
			}
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
		before, err := passFeedRows(tx, now, share)
		if err != nil {
			return err
		}
		ss, err := tx.Sessions(store.SessionFilter{IncludeEnded: true})
		if err != nil {
			return err
		}
		byID := make(map[string]int)
		for i, s := range ss {
			if s.Adapter == "claude" {
				byID[s.ExternalID] = i
			}
		}
		changed := make(map[int]bool)
		for _, f := range b.Facts {
			i, ok := byID[f.SessionID]
			if !ok {
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
			}
			if err := tx.SetIntakeState(s.ID, state); err != nil {
				return err
			}
			event := model.Event{MachineID: tx.MachineID(), Adapter: "claude", ExternalID: s.ExternalID, TS: f.TS, Kind: "otlp:" + f.Name, Summary: scrub.Snippet(f.Summary)}
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
		idx := derive.NewProcIndex(procs)
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
			in := derive.StateInput{Obs: s.SessionObs, LastUserPrompt: s.LastPromptAt, Unseen: unseen[s.ID]}
			if p, ok := idx.Get(s.PID); ok && (s.ProcStart.IsZero() || derive.SameStart(p.StartTime, s.ProcStart)) {
				in.Proc = &p
			}
			if err := intakeInput(tx, *s, events, &in, now); err != nil {
				return err
			}
			s.Label, s.Heuristic, s.Evidence = derive.Label(in, now, derive.Thresholds{StuckAfter: d.Cfg.StuckAfter})
			// Fold activity after labeling so the optional fact can support idle registries.
			if in.LastToolResult != nil && in.LastToolResult.After(s.LastActivityAt) {
				s.LastActivityAt = *in.LastToolResult
			}
			if _, err := tx.UpsertSession(*s); err != nil {
				return err
			}
		}
		for key, n := range map[string]int{"received": len(b.Facts) + b.Rejected, "dropped": b.Dropped, "unmatched": unmatched} {
			raw, err := tx.Meta("otlp:" + key)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			v, _ := strconv.Atoi(raw)
			if err := tx.SetMeta("otlp:"+key, strconv.Itoa(v+n)); err != nil {
				return err
			}
		}
		if len(b.Facts)+b.Rejected > 0 {
			if err := tx.SetSourceOK("otlp", now); err != nil {
				return err
			}
		}
		after, err := passFeedRows(tx, now, share)
		if err != nil {
			return err
		}
		return appendPassChanges(tx, before, after, now)
	})
	return unmatched, err
}
