package collector

import (
	"context"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

func TestClaudeBackgroundMainEraSpareRetirement(t *testing.T) {
	for _, origin := range []string{"interactive", "headless", "interactive/temporary"} {
		t.Run(origin, func(t *testing.T) {
			d, st, p, a := setup(t)
			p.procs = backgroundProcs(t)
			spare := model.SessionObs{Adapter: "claude", ExternalID: "legacy-spare", Origin: origin, PID: p.procs[2].PID, ProcStart: p.procs[2].StartTime, RegistryStatus: "idle", Hosted: true, TurnOpen: true, LastActivityAt: d.Now()}
			claimed := spare
			claimed.ExternalID, claimed.RegistryStatus = "claimed", "busy"
			claimed.PID, claimed.ProcStart = p.procs[3].PID, p.procs[3].StartTime
			if err := st.Tx(context.Background(), func(tx *store.Tx) error {
				for _, o := range []model.SessionObs{spare, claimed} {
					if _, err := tx.UpsertSession(model.Session{SessionObs: o}); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			spare.Origin, claimed.Origin, claimed.RegistryStatus = "background", "background", "idle"
			a.obs = []model.SessionObs{spare, claimed}
			for range 3 {
				d.State = NewState()
				pass(t, d, st, Plan{Procs: true})
				ss := sessions(t, st)
				if len(ss) != 2 {
					t.Fatalf("upgrade must retain both historical rows: %+v", ss)
				}
				for _, s := range ss {
					if s.ExternalID == spare.ExternalID {
						if s.PID != 0 || s.Hosted || s.TurnOpen || s.EndedAt == nil || s.RegistryStatus != "ended" {
							t.Errorf("main-era spare retained ownership: %+v", s)
						}
					} else if s.ExternalID == claimed.ExternalID {
						if s.PID != claimed.PID || s.EndedAt != nil || !s.ConversationClaimed {
							t.Errorf("main-era claimed conversation lost: %+v", s)
						}
					} else {
						t.Errorf("unexpected session: %+v", s)
					}
				}
			}
		})
	}
}

func TestClaudeBackgroundTurnHooksClaimConversation(t *testing.T) {
	for _, event := range []string{"UserPromptSubmit", "Stop", "PreToolUse", "PostToolUse", "PostToolUseFailure", "PermissionRequest", "SessionStart", "SessionEnd", "Notification", "unknown"} {
		claimed := event != "SessionStart" && event != "SessionEnd" && event != "Notification" && event != "unknown"
		for _, hookFirst := range []bool{false, true} {
			name := event + "/same-poll"
			if hookFirst {
				name = event + "/hook-first"
			}
			t.Run(name, func(t *testing.T) {
				d, st, p, a := setup(t)
				p.procs = backgroundProcs(t)
				registry := model.SessionObs{Adapter: "claude", ExternalID: "hook-conversation", Origin: "background", PID: p.procs[2].PID, ProcStart: p.procs[2].StartTime, RegistryStatus: "idle", RegistryUpdatedAt: d.Now(), LastActivityAt: d.Now()}
				a.obs = nil
				if !hookFirst {
					a.obs = []model.SessionObs{registry}
				} else {
					// Spool-only intake expects a collector baseline and machine row.
					pass(t, d, st, Plan{Procs: true})
				}
				if err := spool.Append(d.SpoolDir, model.HookEvent{Adapter: "claude", SessionID: registry.ExternalID, Event: event, TS: d.Now().Add(-time.Second), PID: registry.PID, ProcStart: registry.ProcStart}); err != nil {
					t.Fatal(err)
				}
				if hookFirst {
					if err := drainOnly(context.Background(), d, st); err != nil {
						t.Fatal(err)
					}
					if ss := sessions(t, st); len(ss) != 1 || ss[0].ConversationClaimed != claimed {
						t.Errorf("hook-only claim was not persisted: %+v", ss)
					}
				}
				a.obs = []model.SessionObs{registry}
				for range 3 {
					d.State = NewState()
					pass(t, d, st, Plan{Procs: true})
					ss := sessions(t, st)
					if !claimed {
						if len(ss) != 1 || ss[0].ConversationClaimed || ss[0].PID != 0 || (ss[0].RegistryStatus != "" && ss[0].RegistryStatus != "ended") {
							t.Fatalf("non-turn hook admitted a spare registry: %+v", ss)
						}
						continue
					}
					if len(ss) != 1 || ss[0].ExternalID != registry.ExternalID || ss[0].PID != registry.PID || ss[0].RegistryStatus != "idle" || ss[0].EndedAt != nil || !ss[0].ConversationClaimed {
						t.Fatalf("idle registry lost hook-proven conversation: %+v", ss)
					}
				}
			})
		}
	}
}
