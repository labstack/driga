package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func outputAction(t *testing.T, s *Store, at time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := s.SaveConversation(ctx, Conversation{ID: "conversation", CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveChatMessage(ctx, ChatMessage{ID: "action", ConversationID: "conversation", Role: "action", CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
}
func TestCommandOutputCapture(t *testing.T) {
	s := openTest(t, "", Memory)
	outputAction(t, s, time.Now())
	ctx := context.Background()
	for i, part := range []string{"head\n", "stderr\n", "tail\n"} {
		if err := s.AppendCommandOutput(ctx, "action", "stdout", i, []byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := s.ReadCommandOutput(ctx, "action", &out); err != nil || out.String() != "head\nstderr\ntail\n" {
		t.Fatal(out.String(), err)
	}
	if err := s.AppendCommandOutput(ctx, "absent", "stdout", 0, []byte("no")); err == nil {
		t.Fatal("orphan output accepted")
	}
	peer := *s
	peer.machineID = "peer"
	out.Reset()
	if err := peer.ReadCommandOutput(ctx, "action", &out); err != nil || out.Len() != 0 {
		t.Fatal("peer output leaked", err)
	}
}
func TestCommandOutputRetention(t *testing.T) {
	for _, operation := range []string{"action", "conversation", "prune", "downgrade"} {
		t.Run(operation, func(t *testing.T) {
			s := openTest(t, "", Memory)
			ctx := context.Background()
			now := time.Now()
			outputAction(t, s, now.Add(-31*24*time.Hour))
			if err := s.AppendCommandOutput(ctx, "action", "stderr", 0, []byte("secret")); err != nil {
				t.Fatal(err)
			}
			var err error
			switch operation {
			case "action":
				_, err = s.db.Exec("DELETE FROM messages WHERE id='action'")
			case "conversation":
				_, err = s.db.Exec("DELETE FROM conversations WHERE id='conversation'")
			case "prune":
				err = s.Tx(ctx, func(tx *Tx) error { return tx.Prune(now) })
			case "downgrade":
				err = s.RedactChat(ctx)
			}
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err = s.ReadCommandOutput(ctx, "action", &out); err != nil || out.Len() != 0 {
				t.Fatal(out.String(), err)
			}
		})
	}
}

func TestCommandOutputMigrationFromSix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.db")
	st, err := Open(path, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	outputAction(t, st, time.Now())
	machine := st.MachineID()
	if err = st.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{"DROP TABLE chat_command_output", "DELETE FROM goose_db_version WHERE version_id=7"} {
		if _, err = db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if reader, err := Open(path, ReadOnly); !errors.Is(err, ErrSchemaBehind) {
		if reader != nil {
			reader.Close()
		}
		t.Fatal("reader accepted unmigrated database", err)
	}
	st, err = Open(path, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	messages, err := st.ChatMessages("conversation")
	if err != nil || len(messages) != 1 || messages[0].ID != "action" || st.MachineID() != machine {
		t.Fatal("migration changed existing history", messages, err)
	}
	ctx := context.Background()
	if err = st.AppendCommandOutput(ctx, "action", "stdout", 0, []byte("after upgrade")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = st.ReadCommandOutput(ctx, "action", &out); err != nil || out.String() != "after upgrade" {
		t.Fatal(out.String(), err)
	}
}
