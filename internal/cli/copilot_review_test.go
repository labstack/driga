package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/inbox"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

func TestCopilotVSCodeAndLargeResult(t *testing.T) {
	for _, tc := range []struct {
		name, event, payload string
		vscode               bool
	}{
		{"vscode", "", `{"session_id":"s","timestamp":"2026-10-20T12:30:00.123Z","cwd":"/repo","hook_event_name":"postToolUse"}`, true},
		{"large", "postToolUse", `{"toolResult":{"text":"` + strings.Repeat("private", 300000) + `"},"sessionId":"s","timestamp":1792499400123,"cwd":"/repo","toolName":"shell"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := hookTestEnv(t)
			old := hookParentIdentity
			t.Cleanup(func() { hookParentIdentity = old })
			hookParentIdentity = func() (int32, time.Time, error) {
				if tc.vscode {
					t.Error("VS Code looked up parent")
				}
				return 123, time.Unix(50, 0), nil
			}
			var out, stderr bytes.Buffer
			code := run([]string{"hook", "copilot", tc.event}, "test", strings.NewReader(tc.payload), &out, &stderr)
			if code != 0 || out.Len() != 0 {
				t.Fatal(code, out.String(), stderr.String())
			}
			hs, _, err := spool.Drain(dir, nil)
			if err != nil || len(hs) != 1 {
				t.Fatal(len(hs), err)
			}
			h := hs[0]
			if h.Event != "postToolUse" || h.SessionID != "s" || strings.Contains(h.Message, "private") {
				t.Fatal(h)
			}
			if tc.vscode && (h.Origin != "vscode" || h.PID != 0 || !h.ProcStart.IsZero() || h.TS.Format(time.RFC3339Nano) != "2026-10-20T12:30:00.123Z") {
				t.Fatal(h)
			}
		})
	}
}

type panicHookReader struct{}

func (panicHookReader) Read([]byte) (int, error) { panic("fixture reader panic") }
func TestHookPanicsAndUnwritableSpoolExitZero(t *testing.T) {
	for _, adapter := range []string{"claude", "codex", "copilot"} {
		t.Run(adapter, func(t *testing.T) {
			dir := hookTestEnv(t)
			var out, stderr bytes.Buffer
			event := "SessionStart"
			if adapter == "copilot" {
				event = "sessionStart"
			}
			if code := run([]string{"hook", adapter, event}, "test", panicHookReader{}, &out, &stderr); code != 0 || out.Len() != 0 {
				t.Fatal(code, out.String())
			}
			log, err := os.ReadFile(filepath.Join(dir, "hook-errors.log"))
			if err != nil || !bytes.Contains(log, []byte("panic")) {
				t.Fatal(string(log), err)
			}
			if err := os.WriteFile(filepath.Join(dir, "file"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("DRIGA_SPOOL_DIR", filepath.Join(dir, "file", "spool"))
			payload := `{"session_id":"s","sessionId":"s","timestamp":1,"cwd":"/repo"}`
			if code := run([]string{"hook", adapter, event}, "test", strings.NewReader(payload), &out, &stderr); code != 0 || out.Len() != 0 {
				t.Fatal(code, out.String())
			}
		})
	}
}
func TestHookRecoversParentPanic(t *testing.T) {
	dir := hookTestEnv(t)
	old := hookParentIdentity
	t.Cleanup(func() { hookParentIdentity = old })
	hookParentIdentity = func() (int32, time.Time, error) { panic("fixture parent panic") }
	var out, stderr bytes.Buffer
	if code := run([]string{"hook", "copilot", "sessionStart"}, "test", strings.NewReader(`{"sessionId":"s","timestamp":1,"cwd":"/repo"}`), &out, &stderr); code != 0 || out.Len() != 0 {
		t.Fatal(code, out.String())
	}
	b, err := os.ReadFile(filepath.Join(dir, "hook-errors.log"))
	if err != nil || !bytes.Contains(b, []byte("panic")) {
		t.Fatal(string(b), err)
	}
}

type copilotVersionRunner struct {
	output string
	err    error
}

func (r copilotVersionRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name != "copilot" || strings.Join(args, " ") != "--version" {
		return nil, errors.New("unexpected command")
	}
	return []byte(r.output), r.err
}
func TestCopilotVersionWarnings(t *testing.T) {
	for _, tc := range []struct {
		version string
		warn    bool
	}{
		{"1.0.97", true}, {"GitHub Copilot CLI 1.0.97.\nRun 'copilot update' to check for updates.\n", true}, {"GitHub Copilot CLI v1.0.9\n", true}, {"1.0.98", false}, {"1.1.0", false}, {"2.0.0", false}, {"nonsense", false}, {"99999999999999999999999.0.0", false},
	} {
		t.Run(tc.version, func(t *testing.T) {
			d, _ := doctorFixture(t)
			d.OS = "linux"
			old := newInboxRunner
			t.Cleanup(func() { newInboxRunner = old })
			newInboxRunner = func() inbox.Runner { return copilotVersionRunner{output: tc.version} }
			checks := Doctor(context.Background(), d)
			found := false
			for _, c := range checks {
				if c.Name == "version:copilot" && c.Status == "warn" {
					found = true
					if !strings.Contains(c.Detail, "1.0.98") {
						t.Fatal(c)
					}
				}
			}
			if found != tc.warn {
				t.Fatal(tc, checks)
			}
			t.Run("hook install", func(t *testing.T) {
				var out bytes.Buffer
				if err := (HooksInstallCmd{HooksOptions{Target: "copilot", DryRun: true}}).Run(&Globals{Stdout: &out}); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(out.String(), "Warning:") != tc.warn {
					t.Fatal(tc, out.String())
				}
			})
		})
	}
}
func TestCopilotSandboxAndNoEventsWarnings(t *testing.T) {
	d, st := doctorFixture(t)
	d.OS = "linux"
	d.CopilotHome = filepath.Join(d.Home, "custom-copilot")
	if err := os.MkdirAll(d.CopilotHome, 0700); err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.ReplaceProcs([]model.Proc{{PID: 123, Cmdline: "copilot"}}) }); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"sandbox":{"enabled":true},"other":{"enabled":"not bool"}}`, `{"sandbox":{"enabled":false}}`, `{`, ""} {
		if err := os.WriteFile(filepath.Join(d.CopilotHome, "settings.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		checks := Doctor(context.Background(), d)
		sandbox := false
		for _, c := range checks {
			if c.Name == "sandbox:copilot" && c.Status == "warn" {
				sandbox = true
			}
		}
		if sandbox != strings.Contains(body, `"enabled":true`) {
			t.Fatal(body, checks)
		}
		requireCheck(t, checks, "events:copilot", "warn", "")
	}
	if err := st.SetMeta("hooks:copilot:received", "1"); err != nil {
		t.Fatal(err)
	}
	for _, c := range Doctor(context.Background(), d) {
		if c.Name == "events:copilot" && c.Status == "warn" {
			t.Fatal(c)
		}
	}
}

func TestCopilotParentMustMatchSignature(t *testing.T) {
	for _, tc := range []struct {
		exe, cmd string
		want     bool
	}{
		{"/usr/bin/zsh", "zsh -c driga hook copilot sessionStart", false},
		{"/opt/copilot", "unusual argv", true},
		{"/usr/bin/node", "node /opt/node_modules/@github/copilot/npm-loader.js", true},
		{"/usr/bin/node", "node /other/app.js", false},
	} {
		t.Run(tc.cmd, func(t *testing.T) {
			hookTestEnv(t)
			old := hookParentProcess
			t.Cleanup(func() { hookParentProcess = old })
			hookParentProcess = func() (model.Proc, error) {
				return model.Proc{PID: 123, StartTime: time.Unix(50, 0), Exe: tc.exe, Cmdline: tc.cmd}, nil
			}
			pid, start, err := hookParentIdentity()
			if err != nil || (pid != 0) != tc.want || (!tc.want && !start.IsZero()) {
				t.Fatal(tc, pid, start, err)
			}
		})
	}
}

func TestHookMalformedArgumentsStillExitZero(t *testing.T) {
	hookTestEnv(t)
	for _, args := range [][]string{{"hook"}, {"hook", "copilot"}, {"hook", "unknown", "event"}} {
		var out, stderr bytes.Buffer
		if code := run(args, "test", strings.NewReader("{}"), &out, &stderr); code != 0 || out.Len() != 0 {
			t.Fatal(args, code, out.String())
		}
	}
}

func TestCopilotVSCodePascalEvent(t *testing.T) {
	dir := hookTestEnv(t)
	payload := `{"session_id":"s","timestamp":"2026-10-20T12:30:00Z","cwd":"/repo","hook_event_name":"PostToolUse"}`
	if err := (HookCmd{Adapter: "copilot"}).Run(&Globals{Stdin: strings.NewReader(payload)}); err != nil {
		t.Fatal(err)
	}
	hs, _, err := spool.Drain(dir, nil)
	if err != nil || len(hs) != 1 || hs[0].Event != "postToolUse" {
		t.Fatal(hs, err)
	}
}

func BenchmarkCopilotHook(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("COPILOT_HOME", filepath.Join(home, "copilot"))
	b.Setenv("DRIGA_DATA_DIR", filepath.Join(home, "data"))
	b.Setenv("DRIGA_SPOOL_DIR", filepath.Join(home, "spool"))
	b.Setenv("DRIGA_CONFIG", filepath.Join(home, "config.toml"))
	old := hookParentProcess
	b.Cleanup(func() { hookParentProcess = old })
	hookParentProcess = func() (model.Proc, error) {
		return model.Proc{PID: 123, StartTime: time.Unix(50, 0), Exe: "/opt/copilot"}, nil
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := (HookCmd{Adapter: "copilot", Event: "postToolUse"}).Run(&Globals{Stdin: strings.NewReader(`{"sessionId":"s","timestamp":1792499400123,"cwd":"/repo","toolName":"shell"}`)}); err != nil {
			b.Fatal(err)
		}
	}
}
