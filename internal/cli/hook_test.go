package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/labstack/driga/internal/spool"
)

func hookTestEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, "spool")
	t.Setenv("HOME", home)
	t.Setenv("DRIGA_SPOOL_DIR", dir)
	t.Setenv("DRIGA_CONFIG", filepath.Join(home, "config.toml"))
	return dir
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

func TestHookCommandFastAndExitZero(t *testing.T) {
	dir := hookTestEnv(t)
	cmd := HookCmd{Adapter: "claude", Event: "Stop"}
	if err := cmd.Run(&Globals{Stdin: strings.NewReader("invalid JSON")}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "hook-errors.log"))
	if err != nil || info.Size() == 0 || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	start := time.Now()
	if err := cmd.Run(&Globals{Stdin: strings.NewReader(`{"session_id":"s","cwd":"/repo","last_assistant_message":"private text"}`)}); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if !testing.Short() && !raceEnabled && elapsed >= 20*time.Millisecond {
		t.Fatalf("hook took %v", elapsed)
	}
	got, _, err := spool.Drain(dir, nil)
	if err != nil || len(got) != 1 || got[0].SessionID != "s" || got[0].Message != "" || got[0].Event != "Stop" {
		t.Fatal(got, err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "blocked"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DRIGA_SPOOL_DIR", filepath.Join(filepath.Dir(dir), "blocked"))
	if err := cmd.Run(&Globals{Stdin: strings.NewReader(`{"session_id":"s"}`)}); err != nil {
		t.Fatal(err)
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
			start := time.Now()
			data, err := hookReadInput(input)
			if err != nil || len(data) != 0 || time.Since(start) >= 100*time.Millisecond {
				t.Fatalf("%s stdin must return quickly: %q, %v, %v", name, data, err, time.Since(start))
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
