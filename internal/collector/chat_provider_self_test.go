package collector

import (
	"context"
	"testing"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

func TestInternalChatExclusionUsesProviderIdentity(t *testing.T) {
	for _, provider := range []string{"claude", "codex", "copilot"} {
		t.Run(provider, func(t *testing.T) {
			d, st, _, a := setup(t)
			a.obs = nil
			for _, agent := range []string{"claude", "codex", "copilot"} {
				obs := model.SessionObs{Adapter: agent, ExternalID: "same-id", Cwd: t.TempDir(), RegistryStatus: "busy", StartedAt: d.now(), LastActivityAt: d.now()}
				a.obs = append(a.obs, obs)
				if err := st.Tx(context.Background(), func(tx *store.Tx) error {
					_, err := tx.UpsertSession(model.Session{SessionObs: obs})
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.SaveConversation(context.Background(), store.Conversation{ID: "internal", Agent: provider, SessionID: "same-id", CreatedAt: d.now(), UpdatedAt: d.now()}); err != nil {
				t.Fatal(err)
			}
			for _, agent := range []string{"claude", "codex", "copilot"} {
				if err := spool.Append(d.SpoolDir, model.HookEvent{Adapter: agent, SessionID: "same-id", Event: "UserPromptSubmit", TS: d.now(), Cwd: t.TempDir()}); err != nil {
					t.Fatal(err)
				}
			}
			pass(t, d, st, Plan{})
			got := sessions(t, st)
			if len(got) != 2 {
				t.Fatalf("sessions = %+v, want both other providers", got)
			}
			for _, session := range got {
				if session.Adapter == provider {
					t.Fatalf("internal %s session was collected: %+v", provider, session)
				}
			}
			// A hook arriving between full passes must remain excluded by the
			// lightweight drain path too, without consuming another provider's ID.
			if err := spool.Append(d.SpoolDir, model.HookEvent{Adapter: provider, SessionID: "same-id", Event: "UserPromptSubmit", TS: d.now(), Cwd: t.TempDir()}); err != nil {
				t.Fatal(err)
			}
			if err := drainOnly(context.Background(), d, st); err != nil {
				t.Fatal(err)
			}
			if got := sessions(t, st); len(got) != 2 {
				t.Fatalf("drain sessions = %+v, want both other providers", got)
			}
			if got, err := st.Events(provider, "same-id", d.now()); err != nil || len(got) != 0 {
				t.Fatalf("internal events = %+v, err=%v", got, err)
			}
		})
	}
}

func TestInternalChatEventsPreserveControlRecords(t *testing.T) {
	identity := store.ChatSession{Agent: "copilot", SessionID: "internal"}
	events := []model.HookEvent{
		{Adapter: "copilot", SessionID: "internal", Event: "UserPromptSubmit"},
		{Adapter: "codex", SessionID: "internal", Event: "UserPromptSubmit"},
		{Adapter: "copilot", SessionID: "internal", Event: "ack"},
		{Adapter: "copilot", SessionID: "internal", Event: "summary"},
	}
	got := internalChatEvents(events, map[store.ChatSession]bool{identity: true})
	if len(got) != 3 || got[0].Adapter != "codex" || got[1].Event != "ack" || got[2].Event != "summary" {
		t.Fatalf("remaining events = %+v", got)
	}
}
