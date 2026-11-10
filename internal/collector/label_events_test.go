package collector

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestLabelEventsPreserveIntakeInputs(t *testing.T) {
	d, st, p, a := setup(t)
	now := d.Now()
	kinds := []string{"SessionStart", "SessionEnd", "UserPromptSubmit", "Stop", "Notification:idle_prompt", "Notification", "PermissionRequest", "PostToolUse", "otlp:tool_result", "otlp:assistant_response", "otlp:user_prompt", "otlp:api_error", "unknown:hook", "otlp:unknown"}
	for _, adapter := range []string{"claude", "codex"} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reverse=%t", adapter, reverse), func(t *testing.T) {
				s := model.Session{SessionObs: a.obs[0]}
				s.Adapter, s.ExternalID = adapter, fmt.Sprint(reverse)
				if err := st.Tx(context.Background(), func(tx *store.Tx) error {
					var err error
					s.ID, err = tx.UpsertSession(s)
					if err != nil {
						return err
					}
					if err := tx.SetIntakeState(s.ID, store.IntakeState{LastDecision: now.Add(-time.Minute), LastAPIRequest: now.Add(-time.Second), LastAPIError: "stored fallback"}); err != nil {
						return err
					}
					for repeat := range 3 {
						for i := range kinds {
							if reverse {
								i = len(kinds) - 1 - i
							}
							e := model.Event{Adapter: s.Adapter, ExternalID: s.ExternalID, Kind: kinds[i], TS: now.Add(-time.Duration(repeat%2) * time.Second), Summary: fmt.Sprintf("message %d/%d", repeat, i)}
							if err := tx.InsertEvent(e); err != nil {
								return err
							}
						}
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				full, err := st.LatestEvents(time.Time{})
				if err != nil {
					t.Fatal(err)
				}
				projected, err := st.LabelEvents()
				if err != nil {
					t.Fatal(err)
				}
				fold := func(events []model.Event) (derive.StateInput, model.Session) {
					var matching []model.Event
					for _, e := range events {
						if e.Adapter == s.Adapter && e.ExternalID == s.ExternalID {
							matching = append(matching, e)
						}
					}
					in := derive.StateInput{Obs: s.SessionObs, Proc: &p.procs[0], Unseen: true}
					if err := intakeInput(st, s, matching, &in, now); err != nil {
						t.Fatal(err)
					}
					out := s
					mixSession(&out, in, d.Cfg.StuckAfter, now)
					return in, out
				}
				wantInput, wantSession := fold(full)
				gotInput, gotSession := fold(projected)
				if !reflect.DeepEqual(gotInput, wantInput) || !reflect.DeepEqual(gotSession, wantSession) {
					t.Fatalf("projection changed label inputs/verdict:\n got %+v\nwant %+v", gotInput, wantInput)
				}
			})
		}
	}
}

type labelEventProbe struct {
	*fakePlatform
	pids []int32
}

func (p *labelEventProbe) ExternalPeers(_ context.Context, pids []int32) (map[int32]model.PeerState, error) {
	p.pids = append(p.pids, pids...)
	return map[int32]model.PeerState{}, nil
}

func (p *labelEventProbe) ProcessOS() string { return "darwin" }

func TestLabelEventsSocketProbeFreshHookOrder(t *testing.T) {
	for _, tc := range []struct {
		name, stored string
		fresh        []string
		wantProbe    bool
	}{
		{"stored end", "SessionEnd", nil, true},
		{"stored start", "SessionStart", nil, false},
		{"fresh start beats stored end", "SessionEnd", []string{"SessionStart"}, false},
		{"fresh end beats stored start", "SessionStart", []string{"SessionEnd"}, true},
		{"last fresh start wins", "SessionEnd", []string{"SessionEnd", "SessionStart"}, false},
		{"last fresh end wins", "SessionStart", []string{"SessionStart", "SessionEnd"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, st, p, a := setup(t)
			now := d.Now()
			p.procs[0].PPID = 1
			probe := &labelEventProbe{fakePlatform: p}
			d.Plat, d.Sigs = probe, nil
			s := model.Session{ID: 1, SessionObs: a.obs[0]}
			if err := st.Tx(context.Background(), func(tx *store.Tx) error {
				var err error
				s.ID, err = tx.UpsertSession(s)
				if err != nil {
					return err
				}
				return tx.InsertEvent(model.Event{Adapter: s.Adapter, ExternalID: s.ExternalID, TS: now, Kind: tc.stored})
			}); err != nil {
				t.Fatal(err)
			}
			state := NewState()
			state.spawn[fmt.Sprintf("%d:%d", s.PID, s.ProcStart.Unix())] = s.ID
			var hooks []model.HookEvent
			for _, kind := range tc.fresh {
				hooks = append(hooks, model.HookEvent{Adapter: s.Adapter, SessionID: s.ExternalID, TS: now, Event: kind})
			}
			if _, err := collectSocketPeers(context.Background(), d, st, state, p.procs, []model.Session{s}, now, now, 0, hooks, nil); err != nil {
				t.Fatal(err)
			}
			if got := len(probe.pids) != 0; got != tc.wantProbe {
				t.Fatalf("probed PIDs=%v, want probe=%t", probe.pids, tc.wantProbe)
			}
		})
	}
}
