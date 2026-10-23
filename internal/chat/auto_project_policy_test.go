package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAutoProjectOncePerTurn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell fixture")
	}
	bin, record := t.TempDir(), filepath.Join(t.TempDir(), "git-calls")
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nprintf 'probe\\n' >> \"$CHAT_GIT_RECORD\"\necho true\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("CHAT_GIT_RECORD", record)
	b, a, _ := fixture(t, true)
	b.opts.Ephemeral = true
	row, err := b.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for turn := range 2 {
		project := t.TempDir()
		if err := b.Send(context.Background(), row.ID, "edit", Context{Project: project}); err != nil {
			t.Fatal(err)
		}
		r := <-a.starts
		if err := b.SetMode(context.Background(), row.ID, Auto); err != nil {
			t.Fatal(err)
		}
		for _, tool := range []string{"Edit", "Write", "Edit"} {
			in := Approval{Conversation: row.ID, Token: r.Token, Tool: tool, Input: json.RawMessage(`{"file_path":"ordinary.txt"}`)}
			if p, err := b.Approve(context.Background(), in); err != nil || p.Behavior != "allow" {
				t.Fatal(p, err)
			}
		}
		raw, err := os.ReadFile(record)
		if err != nil {
			t.Fatal(err)
		}
		if calls := strings.Count(string(raw), "probe\n"); calls != turn+1 {
			t.Fatalf("turn %d: %d git probes", turn, calls)
		}
		a.release <- struct{}{}
		settled(t, b, row.ID)
	}
}
