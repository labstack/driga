package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/adapter/claude"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/gitscan"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

func TestHookUpdatesRunningCollector(t *testing.T) {
	spoolDir := hookTestEnv(t)
	home := os.Getenv("HOME")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("COPILOT_HOME", "")
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))
	t.Setenv("GEMINI_CLI_HOME", filepath.Join(home, "gemini"))
	project := filepath.Join(home, "project")
	logs := filepath.Join(home, ".claude", "projects", "synthetic")
	for _, dir := range []string{project, logs, config.DataDir(), spoolDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	// A real Claude JSONL transcript, with a completed assistant turn and no registry.
	var transcript strings.Builder
	for _, row := range []map[string]any{
		{"type": "user", "sessionId": "b-fixture", "cwd": project, "timestamp": now.Add(-time.Minute), "message": map[string]any{"content": "Fixture prompt"}},
		{"type": "assistant", "sessionId": "b-fixture", "timestamp": now.Add(-50 * time.Second), "message": map[string]any{"content": []any{}, "stop_reason": "end_turn"}},
	} {
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		transcript.Write(raw)
		transcript.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(logs, "b-fixture.jsonl"), []byte(transcript.String()), 0600); err != nil {
		t.Fatal(err)
	}
	if err := spool.Append(spoolDir, model.HookEvent{Adapter: "claude", SessionID: "b-fixture", Event: "Stop", TS: now.Add(-30 * time.Second), Cwd: project}); err != nil {
		t.Fatal(err)
	}
	sigs, err := signatures.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	d := collector.Deps{Cfg: config.Default(), Plat: &cliSeqPlatform{snaps: [][]model.Proc{nil}}, Adapters: []adapter.Adapter{claude.New()}, Sigs: sigs, Git: gitscan.NewScanner(1), SpoolDir: spoolDir, State: collector.NewState()}
	// Model a collector that has already established its import baseline.
	seed, err := store.Open(filepath.Join(config.DataDir(), "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.SetMeta("baseline_at", now.Add(-time.Hour).Format(time.RFC3339Nano)); err != nil {
		seed.Close()
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx, d, config.DataDir()) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("collector did not stop")
		}
	})
	var st *store.Store
	var lastSessions []model.Session
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, err = store.Open(filepath.Join(config.DataDir(), "driga.db"), store.ReadOnly)
		if err == nil {
			ss, err := st.Sessions(store.SessionFilter{IncludeEnded: true})
			lastSessions = ss
			if err == nil && len(ss) == 1 && ss[0].Label == "finished" {
				break
			}
			st.Close()
			st = nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st == nil {
		t.Fatalf("fixture session was not collected as finished: sessions=%+v", lastSessions)
	}
	defer st.Close()
	payload, _ := json.Marshal(map[string]any{"session_id": "b-fixture", "notification_type": "permission_prompt", "message": "Fixture permission", "cwd": project})
	started := time.Now()
	// The installed command's capture entry point, including payload parsing/scrubbing.
	if err := (HookCmd{Adapter: "claude", Event: "Notification"}).Run(&Globals{Stdin: strings.NewReader(string(payload))}); err != nil {
		t.Fatal(err)
	}
	// Allow scheduler jitter around the existing one-second spool poll. Once
	// the event is visible, its label and inbox must already agree.
	for time.Since(started) < 1500*time.Millisecond {
		events, err := st.Events("claude", "b-fixture", time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		// Read labels after events so a concurrent commit cannot combine an old
		// label snapshot with a newly visible event in this assertion.
		ss, err := st.Sessions(store.SessionFilter{IncludeEnded: true})
		if err != nil {
			t.Fatal(err)
		}
		items, err := st.Inbox(time.Time{}, false)
		if err != nil {
			t.Fatal(err)
		}
		notification, waiting := false, false
		for _, e := range events {
			if e.Kind == "Notification" {
				notification = true
			}
		}
		for _, i := range items {
			if i.Kind == "waiting" {
				waiting = true
			}
		}
		if notification {
			if len(ss) != 1 || ss[0].Label != "waiting" || derive.SessionMixer(ss[0]).Verdict != "waiting" || !waiting {
				t.Fatalf("committed Notification disagrees with label/mixer/inbox: sessions=%+v items=%+v", ss, items)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("hook event was not ingested within the spool wake interval")
}
