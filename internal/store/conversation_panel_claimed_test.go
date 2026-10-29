package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestConversationPanelTurnHookClaimBackfill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "driga.db")
	st, err := Open(path, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	want := map[string]bool{
		"UserPromptSubmit": true, "Stop": true, "PreToolUse": true,
		"PostToolUse": true, "PostToolUseFailure": true, "PermissionRequest": true,
		"SessionStart": false, "SessionEnd": false, "Notification": false,
		"Notification:idle_prompt": false, "unknown": false,
		"other-adapter": false, "other-session": false, "other-machine": false,
		"non-claude-session": false, "pid:123": false, "": false,
	}
	if err := st.Tx(context.Background(), func(tx *Tx) error {
		for id := range want {
			adapter := "claude"
			if id == "non-claude-session" {
				adapter = "codex"
			}
			if _, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{
				Adapter: adapter, ExternalID: id, Origin: "background", RegistryStatus: "idle",
			}}); err != nil {
				return err
			}
			e := model.Event{Adapter: adapter, ExternalID: id, Kind: id, TS: at}
			machine := tx.machineID
			switch id {
			case "other-adapter":
				e.Adapter, e.Kind = "codex", "Stop"
			case "other-session":
				e.ExternalID, e.Kind = "different-session", "Stop"
			case "other-machine":
				machine, e.Kind = "peer", "Stop"
			case "non-claude-session", "pid:123", "":
				e.Kind = "Stop"
			}
			if id == "Stop" {
				// Exercise real retention: later non-turn events evict the hook
				// from the raw tail while its panel identity survives.
				if err := tx.InsertEvent(e); err != nil {
					return err
				}
				for i := 1; i <= 200; i++ {
					if err := tx.InsertEvent(model.Event{Adapter: "claude", ExternalID: id,
						Kind: "Notification", TS: at.Add(time.Duration(i) * time.Second)}); err != nil {
						return err
					}
				}
				continue
			}
			// Seed historical panel rows directly to cover all six kinds and
			// negative controls, including kinds current panel intake omits.
			if err := tx.exec("INSERT INTO panel_events(machine_id,adapter,external_id,ts,kind) VALUES(?,?,?,?,?)",
				machine, e.Adapter, e.ExternalID, unix(e.TS), e.Kind); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := st.Events("claude", "Stop", time.Time{})
	if err != nil || len(raw) != 200 {
		t.Fatalf("raw tail: %d events, %v", len(raw), err)
	}
	for _, e := range raw {
		if e.Kind != "Notification" {
			t.Fatalf("hook not evicted from raw tail: %+v", e)
		}
	}
	panel, err := st.PanelEvents("claude", "Stop", time.Time{})
	if err != nil || len(panel) != 1 || panel[0].Kind != "Stop" {
		t.Fatalf("retained panel hook: %+v, %v", panel, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	// Recreate the previous schema, then apply the actual embedded migration.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"ALTER TABLE sessions DROP COLUMN conversation_claimed", "DELETE FROM goose_db_version WHERE version_id=6"} {
		if _, err := db.Exec(query); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ss, err := st.Sessions(SessionFilter{IncludeEnded: true})
	if err != nil || len(ss) != len(want) {
		t.Fatalf("upgraded rows: %+v, %v", ss, err)
	}
	for _, s := range ss {
		if s.ConversationClaimed != want[s.ExternalID] {
			t.Errorf("panel-only %q claimed=%t, want %t", s.ExternalID, s.ConversationClaimed, want[s.ExternalID])
		}
	}
}
