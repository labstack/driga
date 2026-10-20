package copilot

import (
	"github.com/labstack/driga/internal/model"
	"testing"
	"time"
)

func TestFoldResumeTitleAndDelayedEvents(t *testing.T) {
	at := time.Unix(100, 0)
	var s model.SessionObs
	Fold(&s, model.HookEvent{Adapter: "copilot", SessionID: "s", TS: at, Event: "sessionStart", Source: "resume", Cwd: "/repo"})
	if s.ExternalID != "s" || s.Origin != "resume" || s.RegistryStatus != "started" {
		t.Fatal(s)
	}
	Fold(&s, model.HookEvent{TS: at.Add(time.Second), Event: "userPromptSubmitted", Title: "first", Message: "prompt"})
	Fold(&s, model.HookEvent{TS: at.Add(2 * time.Second), Event: "userPromptSubmitted", Title: "second"})
	if s.Title != "first" || !s.TurnOpen {
		t.Fatal(s)
	}
	Fold(&s, model.HookEvent{TS: at, Event: "agentStop"})
	if !s.TurnOpen || s.RegistryStatus != "busy" {
		t.Fatal("delayed event regressed state", s)
	}
}

func TestNotificationsDoNotRefreshActivity(t *testing.T) {
	at := time.Unix(100, 0)
	for _, typ := range []string{"agent_idle", "agent_completed", "shell_completed", "shell_detached_completed", "permission_prompt", "elicitation_dialog"} {
		s := model.SessionObs{Adapter: "copilot", ExternalID: "s", LastActivityAt: at, RegistryStatus: "busy"}
		Fold(&s, model.HookEvent{TS: at.Add(time.Hour), Event: "notification", NotificationType: typ})
		if !s.LastActivityAt.Equal(at) {
			t.Fatal(typ, s)
		}
	}
}

func TestSessionStartPreservesTurnAndWait(t *testing.T) {
	at := time.Unix(100, 0)
	for _, state := range []string{"busy", "waiting", "idle", "error", "ended"} {
		for _, offset := range []time.Duration{-time.Second, time.Second} {
			s := model.SessionObs{Adapter: "copilot", ExternalID: "s", RegistryStatus: state, TurnOpen: state == "busy", LastActivityAt: at}
			Fold(&s, model.HookEvent{TS: at.Add(offset), Event: "sessionStart", Source: "resume"})
			if s.RegistryStatus != state || s.TurnOpen != (state == "busy") || s.Origin != "resume" || !s.StartedAt.Equal(at.Add(offset)) || !s.LastActivityAt.Equal(at) {
				t.Fatal(state, offset, s)
			}
		}
	}
}

func TestRecoverableErrorSameSessionActivity(t *testing.T) {
	at := time.Unix(100, 0)
	for _, event := range []string{"userPromptSubmitted", "postToolUse", "postToolUseFailure", "subagentStart", "subagentStop"} {
		s := model.SessionObs{Adapter: "copilot", ExternalID: "s", RegistryStatus: "busy", TurnOpen: true, LastActivityAt: at}
		Fold(&s, model.HookEvent{TS: at.Add(time.Second), Event: "errorOccurred", Recoverable: true})
		if s.TurnOpen || s.RegistryStatus != "error_recoverable" {
			t.Fatal(s)
		}
		Fold(&s, model.HookEvent{TS: at.Add(2 * time.Second), Event: "notification", NotificationType: "agent_idle"})
		if s.TurnOpen || s.RegistryStatus != "error_recoverable" {
			t.Fatal("passive notification revived turn", s)
		}
		Fold(&s, model.HookEvent{TS: at.Add(3 * time.Second), Event: event})
		if !s.TurnOpen || s.RegistryStatus != "busy" {
			t.Fatal(event, s)
		}
		Fold(&s, model.HookEvent{TS: at.Add(time.Second), Event: "errorOccurred"})
		if !s.TurnOpen || s.RegistryStatus != "busy" {
			t.Fatal("late error overrode activity", s)
		}
	}
}
