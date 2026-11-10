package chat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const codexSession = "35a3c7c8-0f2a-4e2d-8187-bc00e17620d6"
const codexStart = `{"type":"thread.started","thread_id":"` + codexSession + `"}` + "\n"

func TestCodexStream(t *testing.T) {
	for _, tc := range []struct {
		name, events, resume string
		want                 string
		fail                 bool
	}{
		{"answer", codexStart + `{"type":"turn.started"}` + "\n" + `{"type":"item.completed","item":{"type":"agent_message","text":"answer"}}` + "\n" + `{"type":"turn.completed"}` + "\n", "", "answer", false},
		{"resume mismatch", codexStart, "f2b09f5c-9172-4271-adab-e8727ee650f5", "", true},
		{"duplicate session", codexStart + codexStart, "", "", true},
		{"native execution", codexStart + `{"type":"item.started","item":{"type":"command_execution"}}` + "\n", "", "", true},
		{"ambient MCP", codexStart + `{"type":"item.started","item":{"type":"mcp_tool_call","server":"personal"}}` + "\n", "", "", true},
		{"unknown event", codexStart + `{"type":"new.unknown"}` + "\n", "", "", true},
		{"failure", codexStart + `{"type":"turn.failed"}` + "\n", "", "", true},
		{"malformed", "broken\n", "", "", true},
		{"completion before init", `{"type":"turn.completed"}` + "\n", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var answer string
			s := codexStream{resume: tc.resume, cancel: cancel, emit: func(d Delta) error { answer += d.Text; return nil }}
			// Exercise fragmented writes, including inside JSON strings.
			var err error
			for _, b := range []byte(tc.events) {
				_, err = s.Write([]byte{b})
				if err != nil {
					break
				}
			}
			if (err != nil) != tc.fail {
				t.Fatalf("error=%v", err)
			}
			if tc.fail && ctx.Err() == nil {
				t.Fatal("invalid stream did not cancel child")
			}
			if answer != tc.want {
				t.Fatalf("answer=%q", answer)
			}
		})
	}
}

func fakeCodex(t *testing.T, body string) (*Codex, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix script fixture")
	}
	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	path := filepath.Join(dir, "codex")
	t.Setenv("CODEX_RECORD", record)
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("DRIGA_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	t.Setenv("DRIGA_SPOOL_DIR", t.TempDir())
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo codex-cli 0.167.0; exit; fi\nprintf '%s\\n' \"$@\" > \"$CODEX_RECORD.args\"\npwd > \"$CODEX_RECORD.cwd\"\nprintf '%s\\n' \"$CODEX_EXEC_SERVER_URL\" \"$DRIGA_CHAT_TOKEN\" \"$DRIGA_INTERNAL\" > \"$CODEX_RECORD.env\"\n/bin/cat > \"$CODEX_RECORD.prompt\"\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return &Codex{Path: path, DataDir: t.TempDir(), Executable: "/fake/driga"}, record
}

func TestCodexLaunchIsolationAndResume(t *testing.T) {
	a, record := fakeCodex(t, "/bin/cat <<'EVENTS'\n"+codexStart+`{"type":"turn.completed"}`+"\nEVENTS")
	r := Request{Prompt: "private question", Resume: codexSession, Directory: t.TempDir(), Socket: "private.sock", Conversation: "conversation", Token: "private-token"}
	if err := a.Run(context.Background(), r, func(Delta) error { return nil }); err != nil {
		t.Fatal(err)
	}
	read := func(suffix string) string {
		b, e := os.ReadFile(record + suffix)
		if e != nil {
			t.Fatal(e)
		}
		return string(b)
	}
	args := read(".args")
	for _, want := range []string{"--ignore-user-config\n", "--ignore-rules\n", "--strict-config\n", "resume\n" + codexSession + "\n-\n"} {
		if !strings.Contains(args, want) {
			t.Fatalf("missing %q in args %s", want, args)
		}
	}
	if strings.Contains(args, r.Token) || strings.Contains(args, r.Prompt) {
		t.Fatal("private token or prompt in argv")
	}
	if got := read(".env"); got != "none\nprivate-token\n1\n" {
		t.Fatalf("env=%q", got)
	}
	if got := strings.TrimSpace(read(".cwd")); got == r.Directory {
		t.Fatal("provider launched in selected project")
	} else if _, err := os.Stat(got); !os.IsNotExist(err) {
		t.Fatalf("scratch survived: %v", err)
	}
	if read(".prompt") != r.Prompt {
		t.Fatal("prompt changed")
	}
	var conf map[string]any
	for _, arg := range strings.Split(args, "\n") {
		if strings.HasPrefix(arg, "mcp_servers=") {
			if err := toml.Unmarshal([]byte(arg), &conf); err != nil {
				t.Fatal(err)
			}
		}
	}
	server := conf["mcp_servers"].(map[string]any)["driga"].(map[string]any)
	var allowed []string
	for _, v := range server["enabled_tools"].([]any) {
		allowed = append(allowed, v.(string))
	}
	for _, bad := range []string{"approve", "kill_process", "replace_dashboard", "create_dashboard"} {
		for _, name := range allowed {
			if name == bad {
				t.Fatalf("unsafe tool %s", bad)
			}
		}
	}
	if !strings.Contains(strings.Join(allowed, ","), "run_command") {
		t.Fatal("missing broker command tool")
	}
}

func TestCodexReadOnlyAndIncomplete(t *testing.T) {
	a, record := fakeCodex(t, "printf '%s\\n' '"+strings.TrimSpace(codexStart)+"'")
	if err := a.Run(context.Background(), Request{ReadOnly: true}, func(Delta) error { return nil }); err == nil {
		t.Fatal("accepted incomplete turn")
	}
	args, err := os.ReadFile(record + ".args")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(args), "run_command") || strings.Contains(string(args), "list_prompts") {
		t.Fatal("read-only launch exposes actions")
	}
}

func TestCodexCompletedTurnRequiresToolResults(t *testing.T) {
	for _, tool := range []string{"get_machine", "show_panels"} {
		t.Run(tool, func(t *testing.T) {
			events := codexStart + `{"type":"item.started","item":{"id":"unfinished","type":"mcp_tool_call","server":"driga","tool":"` + tool + `","arguments":{}}}` + "\n" + `{"type":"turn.completed"}` + "\n"
			a, _ := fakeCodex(t, "/bin/cat <<'EVENTS'\n"+events+"EVENTS")
			var panels []string
			err := a.Run(context.Background(), Request{ReadOnly: true}, func(d Delta) error {
				if len(d.Panel) > 0 {
					panels = append(panels, string(d.Panel))
				}
				return nil
			})
			if err == nil || !strings.Contains(err.Error(), "unfinished tool") {
				t.Fatalf("accepted unfinished tool: %v", err)
			}
			if tool == "show_panels" && (len(panels) != 1 || !strings.Contains(panels[0], `"status":"unknown"`)) {
				t.Fatalf("lost unknown panel outcome: %v", panels)
			}
		})
	}
}

func TestCodexPanelAndToolIdentity(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	var panels []Delta
	emit := func(d Delta) error { panels = append(panels, d); return nil }
	s := codexStream{emit: emit, cancel: cancel, allowed: map[string]bool{"show_panels": true}, cards: stream{emit: emit, cancel: cancel}}
	started := `{"type":"item.started","item":{"id":"call1","type":"mcp_tool_call","server":"driga","tool":"show_panels","arguments":{"panels":[]}}}` + "\n"
	completed := `{"type":"item.completed","item":{"id":"call1","type":"mcp_tool_call","server":"driga","tool":"show_panels","result":{"content":[{"type":"text","text":"ok"}]}}}` + "\n"
	if _, err := s.Write([]byte(codexStart + started + completed)); err != nil {
		t.Fatal(err)
	}
	if len(panels) != 3 || len(panels[2].Panel) == 0 || !strings.Contains(string(panels[2].Panel), `"tool":"show_panels"`) {
		t.Fatalf("missing panel: %+v", panels)
	}
	if _, err := s.Write([]byte(completed)); err == nil {
		t.Fatal("accepted duplicate tool completion")
	}
	for _, name := range []string{"approve", "run_command", "replace_dashboard"} {
		s := codexStream{emit: emit, cancel: cancel, allowed: map[string]bool{"show_panels": true}}
		line := `{"type":"item.started","item":{"id":"disabled","type":"mcp_tool_call","server":"driga","tool":"` + name + `"}}` + "\n"
		if _, err := s.Write([]byte(codexStart + line)); err == nil {
			t.Fatalf("accepted disabled %s", name)
		}
	}
}

func TestCodexCancellationAndCallbackFailure(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		a, _ := fakeCodex(t, "/bin/sleep 60")
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		start := time.Now()
		if err := a.Run(ctx, Request{}, func(Delta) error { return nil }); err == nil {
			t.Fatal("cancel succeeded")
		}
		if time.Since(start) > 3*time.Second {
			t.Fatal("child not bounded")
		}
	})
	t.Run("callback", func(t *testing.T) {
		a, _ := fakeCodex(t, "printf '%s\\n' '"+strings.TrimSpace(codexStart)+"'\n/bin/sleep 60")
		want := errors.New("store failed")
		if err := a.Run(context.Background(), Request{}, func(Delta) error { return want }); !errors.Is(err, want) {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestCodexUnauditedVersionsNeverReceivePrompt(t *testing.T) {
	for _, version := range []string{"0.166.0", "0.167.1", "0.168.0", "0.167.0-beta", "unknown"} {
		t.Run(version, func(t *testing.T) {
			a, record := fakeCodex(t, "exit 0")
			raw, err := os.ReadFile(a.Path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(a.Path, []byte(strings.Replace(string(raw), "codex-cli 0.167.0", "codex-cli "+version, 1)), 0700); err != nil {
				t.Fatal(err)
			}
			if err := a.Run(context.Background(), Request{Prompt: "do not send"}, func(Delta) error { return nil }); err == nil {
				t.Fatal("accepted unaudited version")
			}
			if _, err := os.Stat(record + ".prompt"); !os.IsNotExist(err) {
				t.Fatalf("prompt dispatched: %v", err)
			}
		})
	}
}
