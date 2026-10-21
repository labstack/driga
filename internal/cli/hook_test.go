package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/logfile"
	"github.com/labstack/driga/internal/spool"
)

func hookTestEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, "spool")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("COPILOT_HOME", filepath.Join(home, ".copilot"))
	t.Setenv("DRIGA_DATA_DIR", filepath.Join(home, "data"))
	t.Setenv("DRIGA_SPOOL_DIR", dir)
	t.Setenv("DRIGA_CONFIG", filepath.Join(home, "config.toml"))
	return dir
}

func TestHookUsesRuntimeSpoolAndStructuredRotatedErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("DRIGA_CONFIG", filepath.Join(home, "config.toml"))
	t.Setenv("DRIGA_SPOOL_DIR", "")
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	cmd := HookCmd{Adapter: "claude", Event: "Stop"}
	if err := cmd.Run(&Globals{Stdin: strings.NewReader(`{"session_id":"new-location"}`)}); err != nil {
		t.Fatal(err)
	}
	events, _, err := spool.Drain(config.SpoolDir(), nil)
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".driga/events")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	path := filepath.Join(config.SpoolDir(), "hook-errors.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", int(logfile.MaxSize))), 0600); err != nil {
		t.Fatal(err)
	}
	hookLogError(fmt.Errorf("bad\nlevel=INFO ghp_secret"))
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"level=ERROR", "event=hook_failed", "err="} {
		if !strings.Contains(string(b), field) {
			t.Fatalf("missing %s in %.300s", field, b)
		}
	}
	if strings.Contains(string(b), "ghp_secret") || strings.Count(string(b), "\n") != 1 {
		t.Fatalf("unsafe diagnostic: %.300s", b)
	}
	if info, err := os.Stat(path + ".1"); err != nil || info.Size() > logfile.MaxSize {
		t.Fatal(info, err)
	}
}

func TestHookIgnoresInternal(t *testing.T) {
	dir := hookTestEnv(t)
	t.Setenv("DRIGA_INTERNAL", "1")
	if err := (HookCmd{Adapter: "claude", Event: "Stop"}).Run(&Globals{Stdin: strings.NewReader(`{"session_id":"internal"}`)}); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(dir); !os.IsNotExist(err) && (err != nil || len(entries) != 0) {
		t.Fatal(entries, err)
	}
}

func TestHookSnippetsAndCodexPayload(t *testing.T) {
	dir := hookTestEnv(t)
	if err := os.WriteFile(os.Getenv("DRIGA_CONFIG"), []byte("capture_text = 'snippets'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := HookCmd{Adapter: "codex", Event: "notify", Payload: []string{"ignored", `{"thread-id":"thread","cwd":"/repo","type":"agent-turn-complete","last-assistant-message":"done ghp_secret\n next"}`}}
	if err := cmd.Run(&Globals{}); err != nil {
		t.Fatal(err)
	}
	got, off, err := spool.Drain(dir, nil)
	if err != nil || len(got) != 1 || got[0].SessionID != "thread" || got[0].Event != "notify" || got[0].Message != "done [redacted] next" {
		t.Fatal(got, err)
	}
	claude := HookCmd{Adapter: "claude", Event: "Notification"}
	if err := claude.Run(&Globals{Stdin: strings.NewReader(`{"session_id":"s","hook_event_name":"Notification","message":"please ghp_secret approve","last_assistant_message":"fallback"}`)}); err != nil {
		t.Fatal(err)
	}
	got, off, err = spool.Drain(dir, off)
	if err != nil || len(got) != 1 || got[0].Message != "please [redacted] approve" {
		t.Fatal(got, err)
	}
	if err := os.WriteFile(os.Getenv("DRIGA_CONFIG"), []byte("bad = ["), 0600); err != nil {
		t.Fatal(err)
	}
	if err := claude.Run(&Globals{Stdin: strings.NewReader(`{"session_id":"s","message":"secret"}`)}); err != nil {
		t.Fatal(err)
	}
	got, _, err = spool.Drain(dir, off)
	if err != nil || len(got) != 1 || got[0].Message != "" {
		t.Fatal(got, err)
	}
}

func TestHookEmptyOversizeAndBlockedInput(t *testing.T) {
	dir := hookTestEnv(t)
	cmd := HookCmd{Adapter: "claude", Event: "Stop"}
	if err := cmd.Run(&Globals{Stdin: strings.NewReader("")}); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Run(&Globals{Stdin: strings.NewReader(`{"session_id":"s","message":"` + strings.Repeat("x", 1<<20) + `"}`)}); err != nil {
		t.Fatal(err)
	}
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Run(&Globals{Stdin: r}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("hook blocked on stdin")
	}
	got, _, err := spool.Drain(dir, nil)
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
}

func TestHookWaitsForSlowStdin(t *testing.T) {
	dir := hookTestEnv(t)
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	written := make(chan error, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, err := io.WriteString(w, `{"session_id":"slow","cwd":"/repo"}`)
		w.Close()
		written <- err
	}()
	if err := (HookCmd{Adapter: "claude", Event: "Stop"}).append(&Globals{Stdin: r}); err != nil {
		t.Fatal(err)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	got, _, err := spool.Drain(dir, nil)
	if err != nil || len(got) != 1 || got[0].SessionID != "slow" {
		t.Fatalf("slow payload not captured: %+v, %v", got, err)
	}
	for _, name := range []string{"empty", "TTY"} {
		t.Run(name, func(t *testing.T) {
			var input io.Reader = strings.NewReader("")
			if name == "TTY" {
				f, err := os.Open(os.DevNull)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				input = f
			}
			data, err := hookReadInput(input)
			if err != nil || len(data) != 0 {
				t.Fatalf("%s stdin: %q, %v", name, data, err)
			}
		})
	}
}

func TestHookKongArguments(t *testing.T) {
	var root struct {
		Hook HookCmd `cmd:""`
	}
	parser, err := kong.New(&root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.Parse([]string{"hook", "codex", "notify", `{"thread-id":"s"}`}); err != nil {
		t.Fatal(err)
	}
	if root.Hook.Adapter != "codex" || root.Hook.Event != "notify" || len(root.Hook.Payload) != 1 {
		t.Fatal(root.Hook)
	}
}

func TestHookPayloadAtLimit(t *testing.T) {
	dir := hookTestEnv(t)
	prefix := `{"session_id":"s","message":"`
	suffix := `"}`
	data := prefix + strings.Repeat("x", (1<<20)-len(prefix)-len(suffix)) + suffix
	if err := (HookCmd{Adapter: "claude", Event: "Stop"}).Run(&Globals{Stdin: strings.NewReader(data)}); err != nil {
		t.Fatal(err)
	}
	got, _, err := spool.Drain(dir, nil)
	if err != nil || len(got) != 1 || got[0].Message != "" {
		t.Fatal(got, err)
	}
}
