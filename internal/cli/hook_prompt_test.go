package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/hooks"
	"github.com/labstack/driga/internal/store"
)

func promptTestBroker(t *testing.T) (*chat.Broker, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	b, err := chat.New(ctx, st, nil, chat.Options{DataDir: config.DataDir()})
	if err != nil {
		t.Fatal(err)
	}
	closeSocket, err := chat.Listen(ctx, config.DataDir(), b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); closeSocket(); b.Close(); st.Close() })
	return b, cancel
}
func hookPromptPending(t *testing.T, b *chat.Broker) chat.Prompt {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ps, err := b.Prompts(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(ps) > 0 {
			return ps[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("hook did not register prompt")
	return chat.Prompt{}
}
func writePromptTranscript(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(text)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
}
func TestPermissionHookBuiltBinary(t *testing.T) {
	readEnv(t)
	bin := filepath.Join(t.TempDir(), "driga")
	build := exec.Command("go", "build", "-o", bin, "../../cmd/driga")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	for _, scenario := range []string{"allow", "deny", "terminal", "session end", "broker stopped", "hook exit", "no broker", "bad payload", "bad transcript"} {
		t.Run(scenario, func(t *testing.T) {
			readEnv(t)
			if err := os.WriteFile(config.Path(), []byte("capture_text = 'snippets'\n"), 0600); err != nil {
				t.Fatal(err)
			}
			// Explicitly exercise configuration namespace validation in a real process.
			if _, err := config.Load(config.Path()); err != nil {
				t.Fatal(err)
			}
			var err error
			var b *chat.Broker
			var stop context.CancelFunc
			if scenario != "no broker" {
				b, stop = promptTestBroker(t)
			}
			path := filepath.Join(t.TempDir(), "transcript.jsonl")
			use := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"old","name":"Bash","input":{"command":"private"}},{"type":"tool_use","id":"current","name":"Bash","input":{"command":"private"}}]}}` + "\n"
			if err = os.WriteFile(path, []byte(use), 0600); err != nil {
				t.Fatal(err)
			}
			if scenario == "bad transcript" {
				path += "missing"
			}
			payload, _ := json.Marshal(permissionPayload{Session: "session", Cwd: "/repo", Transcript: path, Tool: "Bash", Input: json.RawMessage(`{"command":"private"}`)})
			if scenario == "bad payload" {
				payload = []byte(`{"tool_input":"private"`)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, "hook", "claude", "PermissionRequest")
			cmd.Stdin = bytes.NewReader(payload)
			var out, stderr bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &stderr
			started := time.Now()
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			if b != nil && scenario != "bad payload" && scenario != "bad transcript" {
				p := hookPromptPending(t, b)
				if string(p.Input) != `{"command":"private"}` || p.Tool != "Bash" || p.Session != "session" {
					t.Fatal(p)
				}
				switch scenario {
				case "allow", "deny":
					if err = b.AnswerPrompt(context.Background(), p.ID, scenario == "allow"); err != nil {
						t.Fatal(err)
					}
					if err = b.AnswerPrompt(context.Background(), p.ID, true); err == nil {
						t.Fatal("double answer")
					}
				case "terminal":
					writePromptTranscript(t, path, `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"old"}]}}`+"\n")
					time.Sleep(300 * time.Millisecond)
					ps, _ := b.Prompts(context.Background())
					if len(ps) != 1 {
						t.Fatal("matched old use")
					}
					result := `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"current"}]}}`
					writePromptTranscript(t, path, result[:len(result)/2])
					time.Sleep(300 * time.Millisecond)
					ps, _ = b.Prompts(context.Background())
					if len(ps) != 1 {
						t.Fatal("partial line retired prompt")
					}
					writePromptTranscript(t, path, result[len(result)/2:]+"\n")
				case "session end":
					end := exec.CommandContext(ctx, bin, "hook", "claude", "SessionEnd")
					end.Stdin = strings.NewReader(`{"session_id":"session","cwd":"/repo"}`)
					if output, err := end.CombinedOutput(); err != nil || len(output) > 0 {
						t.Fatal(string(output), err)
					}
				case "broker stopped":
					stop()
				case "hook exit":
					cancel()
				}
			}
			select {
			case err = <-done:
				if err != nil && scenario != "hook exit" {
					t.Fatal(err, stderr.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("hook did not exit")
			}
			if scenario == "allow" || scenario == "deny" {
				want := `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}` + "\n"
				if scenario == "deny" {
					want = `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"deny","message":"Denied from driga"}}}` + "\n"
				}
				if out.String() != want {
					t.Fatalf("output %q, want %q", out.String(), want)
				}
			} else if out.Len() != 0 {
				t.Fatal("failure emitted output", out.String())
			}
			if stderr.Len() != 0 {
				t.Fatal("hook diagnostics", stderr.String())
			}
			if scenario == "no broker" && time.Since(started) > time.Second {
				t.Fatal("absent broker was slow")
			}
			if b != nil {
				deadline := time.Now().Add(2 * time.Second)
				for {
					ps, _ := b.Prompts(context.Background())
					if len(ps) == 0 {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("prompt retained after hook exit")
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			_ = filepath.WalkDir(config.SpoolDir(), func(path string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					data, _ := os.ReadFile(path)
					if bytes.Contains(data, []byte("private")) {
						t.Fatal("prompt input reached spool", path)
					}
				}
				return nil
			})
		})
	}
}
func TestPermissionHookNoBrokerFast(t *testing.T) {
	readEnv(t)
	var out bytes.Buffer
	start := time.Now()
	if err := (HookCmd{Adapter: "claude", Event: "PermissionRequest"}).Run(&Globals{Stdin: strings.NewReader(`private`), Stdout: &out}); err != nil || out.Len() != 0 {
		t.Fatal(err, out.String())
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("no-broker hook exceeded dial budget")
	}
}
func TestPromptTranscriptMatching(t *testing.T) {
	for _, tc := range []struct {
		name, tail string
		answered   bool
	}{
		{"wrong input", `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"wrong","name":"Bash","input":{"command":"different"}}]}}` + "\n" + `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"wrong"}]}}` + "\n", false},
		{"result before use", `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"one"}]}}` + "\n" + `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"one","name":"Bash","input":{"b":2,"command":"private"}}]}}` + "\n", false},
		{"canonical input", `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"one","name":"Bash","input":{"b":2,"command":"private"}}]}}` + "\n" + `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"one"}]}}` + "\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "transcript")
			if err := os.WriteFile(path, []byte(tc.tail), 0600); err != nil {
				t.Fatal(err)
			}
			w, err := newPromptTranscript(permissionPayload{Transcript: path, Tool: "Bash", Input: json.RawMessage(`{"command":"private","b":2}`)})
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			got, err := w.answered()
			if err != nil || got != tc.answered {
				t.Fatal(got, err)
			}
		})
	}
}

func TestClaudePromptHookStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = hooks.ClaudeInstall(path, bin, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	status, _, err := hookStatus("claude", path, bin)
	if err != nil || status != "ok" {
		t.Fatal(status, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err = json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	delete(settings["hooks"].(map[string]any), "PermissionRequest")
	raw, _ = json.Marshal(settings)
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	status, _, err = hookStatus("claude", path, bin)
	if err != nil || status != "warn" {
		t.Fatal("missing prompt hook should need repair", status, err)
	}
}
