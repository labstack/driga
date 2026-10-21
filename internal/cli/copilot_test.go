package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/hooks"
	"github.com/labstack/driga/internal/spool"
)

func TestCopilotCapturePolicies(t *testing.T) {
	for _, policy := range []string{"titles", "snippets"} {
		t.Run(policy, func(t *testing.T) {
			dir := hookTestEnv(t)
			t.Setenv("COPILOT_HOME", t.TempDir())
			if err := os.WriteFile(config.Path(), []byte("capture_text = \""+policy+"\"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			payload := `{"sessionId":"s","timestamp":1792512000123,"cwd":"/repo","prompt":"Fix token ghp_abcdefghijklmnopqrstuvwxyz1234567890","toolName":"shell","toolArgs":{"command":"echo private"},"message":"private notification"}`
			if err := (HookCmd{Adapter: "copilot", Event: "userPromptSubmitted"}).Run(&Globals{Stdin: strings.NewReader(payload), Stdout: &out}); err != nil || out.Len() != 0 {
				t.Fatal(err, out.String())
			}
			got, _, err := spool.Drain(dir, nil)
			if err != nil || len(got) != 1 {
				t.Fatal(got, err)
			}
			h := got[0]
			if h.SessionID != "s" || h.PID != 0 || !h.TS.Equal(time.UnixMilli(1792512000123)) {
				t.Fatal(h)
			}
			// Process APIs can be denied by the sandbox; the injected identity is checked separately.
			if h.Tool != "shell" {
				t.Fatal(h)
			}
			if policy == "titles" && (h.Title != "" || h.Prompt != "") {
				t.Fatal(h)
			}
			if policy == "snippets" && (!strings.HasPrefix(h.Title, "Fix token") || strings.Contains(h.Title, "ghp_abcdefghijklmnopqrstuvwxyz")) {
				t.Fatal(h)
			}
			if policy == "titles" && (h.Message != "" || h.Detail != "") {
				t.Fatal(h)
			}
			if policy == "snippets" && (h.Detail != "echo private" || h.Message != "private notification") {
				t.Fatal(h)
			}
		})
	}
}

func TestCopilotCaptureFailOpen(t *testing.T) {
	for _, payload := range []string{`{`, `{}`, `{"sessionId":"s","timestamp":"bad"}`, `{"sessionId":"s"}` + strings.Repeat(" ", hookPayloadLimit), `{"sessionId":"s","timestamp":1,"cwd":"/repo"} true`} {
		dir := hookTestEnv(t)
		t.Setenv("COPILOT_HOME", t.TempDir())
		var out bytes.Buffer
		if err := (HookCmd{Adapter: "copilot", Event: "notification"}).Run(&Globals{Stdin: strings.NewReader(payload), Stdout: &out}); err != nil || out.Len() != 0 {
			t.Fatal(err, out.String())
		}
		got, _, err := spool.Drain(dir, nil)
		if err != nil || len(got) != 0 {
			t.Fatal(got, err)
		}
	}
}

func TestCopilotHooksCLIAndDoctor(t *testing.T) {
	hookTestEnv(t)
	home := os.Getenv("HOME")
	t.Setenv("COPILOT_HOME", filepath.Join(home, "copilot-custom"))
	path := hooks.CopilotPath(home, os.Getenv("COPILOT_HOME"))
	var out bytes.Buffer
	opts := HooksOptions{Target: "copilot"}
	if err := (HooksInstallCmd{opts}).Run(&Globals{Stdin: strings.NewReader("n\n"), Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("decline wrote", err)
	}
	out.Reset()
	if err := (HooksInstallCmd{opts}).Run(&Globals{Stdin: strings.NewReader("yes\n"), Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Apply?") {
		t.Fatal(out.String())
	}
	exe, _ := os.Executable()
	status, err := hooks.CopilotStatus(path, stablePath(exe))
	if err != nil || status != "installed" {
		t.Fatal(status, err)
	}
	installed, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(installed, []byte(`"errorOccurred"`)) {
		t.Fatal("missing error hook", string(installed), err)
	}
	d, _ := doctorFixture(t)
	d.Home = home
	d.CopilotHome = filepath.Dir(filepath.Dir(path))
	t.Setenv("COPILOT_HOME", d.CopilotHome)
	requireCheck(t, Doctor(context.Background(), d), "hooks:copilot", "ok", "")
	if _, err := hooks.CopilotInstall(path, filepath.Join(t.TempDir(), "old", "driga"), false, time.Now()); err != nil {
		t.Fatal(err)
	}
	c := requireCheck(t, Doctor(context.Background(), d), "hooks:copilot", "warn", "driga hooks install copilot")
	if !strings.Contains(c.Detail, "stale") {
		t.Fatal(c)
	}
	out.Reset()
	if err := (HooksUninstallCmd{opts}).Run(&Globals{Stdin: strings.NewReader("y\n"), Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "until restarted") {
		t.Fatal(out.String())
	}
	requireCheck(t, Doctor(context.Background(), d), "hooks:copilot", "warn", "driga hooks install copilot")
	for _, p := range []string{filepath.Join(home, ".claude", "settings.json"), filepath.Join(home, ".codex", "config.toml")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatal("touched other agent", p)
		}
	}
}

func TestCopilotPermissionToolDoesNotUseLastTool(t *testing.T) {
	dir := hookTestEnv(t)
	t.Setenv("COPILOT_HOME", t.TempDir())
	for _, message := range []string{"Copilot needs your permission to use shell", "Approve?"} {
		b, _ := json.Marshal(map[string]any{"sessionId": "s", "timestamp": 1, "cwd": "/repo", "message": message, "notification_type": "permission_prompt"})
		if err := (HookCmd{Adapter: "copilot", Event: "notification"}).Run(&Globals{Stdin: bytes.NewReader(b)}); err != nil {
			t.Fatal(err)
		}
	}
	got, _, err := spool.Drain(dir, nil)
	if err != nil || len(got) != 2 || got[0].Tool != "" || got[1].Tool != "" {
		t.Fatal(got, err)
	}
}

func TestCopilotCaptureParentIdentityAndCLIExit(t *testing.T) {
	dir := hookTestEnv(t)
	old := hookParentIdentity
	t.Cleanup(func() { hookParentIdentity = old })
	start := time.Unix(50, 0)
	hookParentIdentity = func() (int32, time.Time, error) { return 123, start, nil }
	var out, stderr bytes.Buffer
	code := run([]string{"hook", "copilot", "sessionStart"}, "test", strings.NewReader(`{"sessionId":"s","timestamp":100000,"cwd":"/repo","source":"resume"}`), &out, &stderr)
	if code != 0 || out.Len() != 0 {
		t.Fatal(code, out.String(), stderr.String())
	}
	hs, _, err := spool.Drain(dir, nil)
	if err != nil || len(hs) != 1 || hs[0].PID != 123 || !hs[0].ProcStart.Equal(start) || hs[0].Source != "resume" {
		t.Fatal(hs, err)
	}
	for _, payload := range []string{`{`, `{}`, strings.Repeat("x", hookPayloadLimit+1)} {
		code = run([]string{"hook", "copilot", "notification"}, "test", strings.NewReader(payload), &out, &stderr)
		if code != 0 || out.Len() != 0 {
			t.Fatal(code, out.String(), stderr.String())
		}
	}
}

func TestCopilotErrorCapturePolicies(t *testing.T) {
	for _, policy := range []string{"titles", "snippets"} {
		for _, recoverable := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/recoverable=%t", policy, recoverable), func(t *testing.T) {
				dir := hookTestEnv(t)
				if err := os.WriteFile(config.Path(), []byte("capture_text = \""+policy+"\"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				payload := fmt.Sprintf(`{"sessionId":"s","timestamp":1792512000123,"cwd":"/repo","error":{"name":"QuotaError ghp_abcdefghijklmnopqrstuvwxyz1234567890","message":"You have exceeded your monthly quota","stack":%q},"errorContext":"private context","recoverable":%t,"reason":"private reason","message":"private message","toolName":"private tool","toolArgs":{"command":"private command"}}`, strings.Repeat("private stack", 100000), recoverable)
				var out, stderr bytes.Buffer
				if code := run([]string{"hook", "copilot", "errorOccurred"}, "test", strings.NewReader(payload), &out, &stderr); code != 0 || out.Len() != 0 || stderr.Len() != 0 {
					t.Fatal(code, out.String(), stderr.String())
				}
				hs, _, err := spool.Drain(dir, nil)
				if err != nil || len(hs) != 1 {
					t.Fatal(hs, err)
				}
				h := hs[0]
				if h.Event != "errorOccurred" || h.Recoverable != recoverable {
					t.Fatal(h)
				}
				if policy == "titles" && (h.Reason != "" || h.Message != "") {
					t.Fatal("titles retained error text", h)
				}
				if policy == "snippets" && (!strings.HasPrefix(h.Reason, "error: QuotaError") || h.Message != h.Reason || strings.Contains(h.Reason, "ghp_abcdefghijklmnopqrstuvwxyz")) {
					t.Fatal("missing scrubbed error name", h)
				}
				b, _ := json.Marshal(h)
				for _, private := range []string{"private", "monthly quota", "stack", "errorContext"} {
					if bytes.Contains(b, []byte(private)) {
						t.Fatalf("retained %q: %s", private, b)
					}
				}
			})
		}
	}
}
