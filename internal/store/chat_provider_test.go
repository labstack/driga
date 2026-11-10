package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestInternalChatSessionsKeepProviderNamespace(t *testing.T) {
	s := openTest(t, "", Memory)
	now := time.Now()
	for i, agent := range []string{"claude", "codex", "copilot", "", "unknown"} {
		if err := s.SaveConversation(context.Background(), Conversation{ID: fmt.Sprint(i), Agent: agent, SessionID: "same-id", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveConversation(context.Background(), Conversation{ID: "not-started", Agent: "claude", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	got, err := s.InternalChatSessions()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[ChatSession]bool{}
	for _, identity := range got {
		seen[identity] = true
	}
	if len(got) != 3 || !seen[ChatSession{"claude", "same-id"}] || !seen[ChatSession{"codex", "same-id"}] || !seen[ChatSession{"copilot", "same-id"}] {
		t.Fatalf("identities = %+v", got)
	}
}

func TestRemoveInternalChatRejectsUnknownIdentity(t *testing.T) {
	s := openTest(t, "", Memory)
	mustTx(t, s, func(tx *Tx) error {
		for _, agent := range []string{"claude", "codex", "copilot", "unknown", ""} {
			if _, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: agent, ExternalID: "same-id"}}); err != nil {
				return err
			}
		}
		for _, agent := range []string{"", "unknown", "claude"} {
			external := "same-id"
			if agent == "claude" {
				external = ""
			}
			if err := tx.RemoveInternalChat(agent, external); err != nil {
				return err
			}
		}
		return nil
	})
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count); err != nil || count != 5 {
		t.Fatalf("remaining sessions = %d, err=%v", count, err)
	}
}

func TestRemoveInternalChatKeepsOtherProvidersAndMachines(t *testing.T) {
	for _, provider := range []string{"claude", "codex", "copilot"} {
		t.Run(provider, func(t *testing.T) {
			s := openTest(t, "", Memory)
			now := time.Now()
			mustTx(t, s, func(tx *Tx) error {
				for _, machine := range []string{s.MachineID(), "remote"} {
					m := tx.ForMachine(machine)
					for _, agent := range []string{"claude", "codex", "copilot"} {
						id, err := m.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: agent, ExternalID: "same-id"}})
						if err != nil {
							return err
						}
						if err := m.UpsertInboxItem(model.InboxItem{ID: agent, SessionID: id, Kind: "finished", TS: now}); err != nil {
							return err
						}
						if err := m.InsertHandoff(id, now, "finished"); err != nil {
							return err
						}
						e := model.Event{Adapter: agent, ExternalID: "same-id", TS: now, Kind: "Stop"}
						if err := m.InsertEvent(e); err != nil {
							return err
						}
						if _, err := m.RecordPanelEvent(e); err != nil {
							return err
						}
					}
				}
				return tx.RemoveInternalChat(provider, "same-id")
			})
			for _, table := range []string{"sessions", "events", "panel_events", "inbox_items", "handoffs"} {
				var local, remote int
				if err := s.db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE machine_id=?", s.MachineID()).Scan(&local); err != nil {
					t.Fatal(err)
				}
				if err := s.db.QueryRow("SELECT COUNT(*) FROM " + table + " WHERE machine_id='remote'").Scan(&remote); err != nil {
					t.Fatal(err)
				}
				if local != 2 || remote != 3 {
					t.Fatalf("%s rows local=%d remote=%d; want 2 and 3", table, local, remote)
				}
			}
		})
	}
}
