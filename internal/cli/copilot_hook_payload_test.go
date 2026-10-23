package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
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
