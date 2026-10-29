package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestConversationTurnHookClaimBackfill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "driga.db")
	st, err := Open(path, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"UserPromptSubmit": true, "Stop": true, "PreToolUse": true,
		"PostToolUse": true, "PostToolUseFailure": true, "PermissionRequest": true,
		"SessionStart": false, "SessionEnd": false, "Notification": false,
		"Notification:idle_prompt": false, "unknown": false,
		"other-adapter": false, "other-session": false, "other-machine": false,
	}
	if err := st.Tx(context.Background(), func(tx *Tx) error {
		for id := range want {
			if _, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: id, Origin: "interactive", RegistryStatus: "idle"}}); err != nil {
				return err
			}
			e := model.Event{Adapter: "claude", ExternalID: id, Kind: id, TS: time.Now()}
			switch id {
			case "other-adapter":
				e.Adapter, e.Kind = "codex", "Stop"
			case "other-session":
				e.ExternalID, e.Kind = "different-session", "Stop"
			case "other-machine":
				e.Kind = "Stop"
				if err := tx.ForMachine("peer").InsertEvent(e); err != nil {
					return err
				}
				continue
			}
			if err := tx.InsertEvent(e); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		st.Close()
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
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
			t.Errorf("%s claimed=%t, want %t", s.ExternalID, s.ConversationClaimed, want[s.ExternalID])
		}
	}
}

func TestConversationClaimEvidenceMigratesAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "driga.db")
	st, err := Open(path, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(context.Background(), func(tx *Tx) error {
		for _, o := range []model.SessionObs{
			{Adapter: "claude", ExternalID: "spare", Origin: "background", RegistryStatus: "idle"},
			{Adapter: "claude", ExternalID: "busy", Origin: "background", RegistryStatus: "busy"},
			{Adapter: "claude", ExternalID: "transcript", Origin: "background", RegistryStatus: "idle", SourcePath: "/fixture/transcript.jsonl"},
		} {
			if _, err := tx.UpsertSession(model.Session{SessionObs: o}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	// Recreate the previous release's schema and rows without the new bit.
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
	if err != nil || len(ss) != 3 {
		t.Fatalf("upgraded rows: %+v, %v", ss, err)
	}
	for _, s := range ss {
		if want := s.ExternalID != "spare"; s.ConversationClaimed != want {
			t.Errorf("%s claimed=%t, want %t", s.ExternalID, s.ConversationClaimed, want)
		}
		s.RegistryStatus = "idle"
		if err := st.Tx(context.Background(), func(tx *Tx) error {
			_, err := tx.UpsertSession(s)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := Open(path, ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	ss, err = reader.Sessions(SessionFilter{IncludeEnded: true})
	if err != nil || len(ss) != 3 {
		t.Fatalf("reopened rows: %+v, %v", ss, err)
	}
	for _, s := range ss {
		if s.ConversationClaimed != (s.ExternalID != "spare") || s.RegistryStatus != "idle" {
			t.Errorf("claim did not survive idle update and reopen: %+v", s)
		}
	}
}
