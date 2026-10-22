package derive

import (
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestNormalAndAbnormalEnds(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	stop := now.Add(-time.Second)
	for _, tc := range []struct {
		name, want string
		in         StateInput
	}{
		{"claude idle exit", "ended", StateInput{Obs: model.SessionObs{Adapter: "claude", RegistryStatus: "idle"}}},
		{"claude busy exit", "dead", StateInput{Obs: model.SessionObs{Adapter: "claude", RegistryStatus: "busy"}}},
		{"claude unbound open transcript", "unknown", StateInput{Obs: model.SessionObs{Adapter: "claude", TurnOpen: true}}},
		{"claude unbound completed transcript", "unknown", StateInput{Obs: model.SessionObs{Adapter: "claude", LastActivityAt: stop}}},
		{"claude unread completion exit", "ended", StateInput{Obs: model.SessionObs{Adapter: "claude", RegistryStatus: "busy"}, LastStop: &stop, Unseen: true}},
		{"claude replaced busy conversation", "ended", StateInput{Obs: model.SessionObs{Adapter: "claude", RegistryStatus: "ended"}, LastLifecycle: "UserPromptSubmit", LastLifecycleAt: stop}},
		{"claude explicit end", "ended", StateInput{Obs: model.SessionObs{Adapter: "claude", PID: 42, RegistryStatus: "busy"}, Proc: &model.Proc{}, LastLifecycle: "SessionEnd", LastLifecycleAt: now}},
		{"claude idle after older telemetry", "ended", StateInput{Obs: model.SessionObs{Adapter: "claude", RegistryStatus: "idle", RegistryUpdatedAt: now}, LastToolResult: &stop}},
		{"claude prompt after idle", "dead", StateInput{Obs: model.SessionObs{Adapter: "claude", RegistryStatus: "idle", RegistryUpdatedAt: stop}, LastUserPrompt: now}},
		{"claude unbound hook prompt", "working", StateInput{Obs: model.SessionObs{Adapter: "claude"}, LastLifecycle: "UserPromptSubmit", LastLifecycleAt: stop}},
		{"codex explicit end", "ended", StateInput{Obs: model.SessionObs{Adapter: "codex", TurnOpen: true}, LastLifecycle: "SessionEnd", LastLifecycleAt: now}},
		{"codex completed exit", "ended", StateInput{Obs: model.SessionObs{Adapter: "codex"}, Unseen: true}},
		{"codex interrupted exit", "dead", StateInput{Obs: model.SessionObs{Adapter: "codex", TurnOpen: true}}},
		{"gemini completed exit", "ended", StateInput{Obs: model.SessionObs{Adapter: "gemini"}}},
		{"gemini interrupted exit", "dead", StateInput{Obs: model.SessionObs{Adapter: "gemini", TurnOpen: true}}},
		{"copilot explicit end", "ended", StateInput{Obs: model.SessionObs{Adapter: "copilot", RegistryStatus: "ended"}}},
		{"copilot idle exit", "ended", StateInput{Obs: model.SessionObs{Adapter: "copilot", RegistryStatus: "idle"}, Unseen: true}},
		{"copilot busy exit", "dead", StateInput{Obs: model.SessionObs{Adapter: "copilot", RegistryStatus: "busy", TurnOpen: true}}},
		{"copilot fatal error", "dead", StateInput{Obs: model.SessionObs{Adapter: "copilot", RegistryStatus: "error"}}},
		{"unknown agy exit", "unknown", StateInput{Obs: model.SessionObs{Adapter: "agy"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := LabelWithEvidence(tc.in, now, Thresholds{})
			if m.Verdict != tc.want || m.Agreement == 0 {
				t.Fatalf("got %+v; want %s with supporting evidence", m, tc.want)
			}
		})
	}
}
