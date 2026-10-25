package collector

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/driga/internal/model"
)

// Driga's own Chat runs claude with a prompt that opens with its preamble;
// the collector keeps that session out, so it never shows as an agent.
func TestChatOwnSessionIsNotCollected(t *testing.T) {
	d, st, _, a := setup(t)
	chat := a.obs[0]
	chat.ExternalID, chat.PID, chat.Title = "chat", 0, model.ChatPreamble+" Explain collected state"
	// Before its first prompt is read, Chat is known by where it runs.
	data := t.TempDir()
	if err := os.WriteFile(filepath.Join(data, "chat.key"), []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	early := a.obs[0]
	early.ExternalID, early.PID, early.Cwd = "early", 0, filepath.Join(data, "chat")
	a.obs = append(a.obs, chat, early)
	pass(t, d, st, Plan{Procs: true})
	var ids []string
	for _, s := range sessions(t, st) {
		ids = append(ids, s.ExternalID)
	}
	if len(ids) != 1 || ids[0] != "one" {
		t.Fatalf("sessions = %v, want only the person's agent", ids)
	}
}
