package collector

import (
	"testing"

	"github.com/labstack/driga/internal/model"
)

// Driga's own Chat runs claude with a prompt that opens with its preamble;
// the collector keeps that session out, so it never shows as an agent.
func TestChatOwnSessionIsNotCollected(t *testing.T) {
	d, st, _, a := setup(t)
	chat := a.obs[0]
	chat.ExternalID, chat.PID, chat.Title = "chat", 0, model.ChatPreamble+" Explain collected state"
	a.obs = append(a.obs, chat)
	pass(t, d, st, Plan{Procs: true})
	var ids []string
	for _, s := range sessions(t, st) {
		ids = append(ids, s.ExternalID)
	}
	if len(ids) != 1 || ids[0] != "one" {
		t.Fatalf("sessions = %v, want only the person's agent", ids)
	}
}
