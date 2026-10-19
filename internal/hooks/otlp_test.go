package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHooksInstallOTLPRefusesExistingTelemetry(t *testing.T) {
	for _, tt := range []struct{ agent, content string }{{"claude", `{"env":{"OTEL_EXPORTER_OTLP_ENDPOINT":"http://elsewhere"}}`}, {"claude", `{"env":{"OTEL_LOG_USER_PROMPTS":"1"}}`}, {"codex", "[otel]\nlog_user_prompt = false\n"}} {
		t.Run(tt.agent+tt.content, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "settings")
			os.WriteFile(p, []byte(tt.content), 0600)
			var e error
			if tt.agent == "claude" {
				_, e = ClaudeInstallOTLP(p, false, false, time.Now())
			} else {
				_, e = CodexInstallOTLP(p, false, false, time.Now())
			}
			if e == nil {
				t.Fatal("expected refusal")
			}
			b, _ := os.ReadFile(p)
			if string(b) != tt.content {
				t.Fatal("changed existing telemetry")
			}
		})
	}
}
func TestHooksOTLPRoundTrip(t *testing.T) {
	for _, a := range []string{"claude", "codex"} {
		t.Run(a, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "settings")
			var d Diff
			var e error
			if a == "claude" {
				d, e = ClaudeInstallOTLP(p, true, false, time.Now())
			} else {
				d, e = CodexInstallOTLP(p, true, false, time.Now())
			}
			if e != nil {
				t.Fatal(e)
			}
			if !d.Changed || strings.Contains(d.After, "OTEL_LOG_USER_PROMPTS") || strings.Contains(d.After, "OTEL_LOG_TOOL_DETAILS") {
				t.Fatal(d)
			}
			if a == "codex" && (!strings.Contains(d.After, "log_user_prompt = false") || !strings.Contains(d.After, "json")) {
				t.Fatal(d.After)
			}
			if a == "claude" {
				d, e = ClaudeUninstallOTLP(p, false)
			} else {
				d, e = CodexUninstallOTLP(p, false)
			}
			if e != nil || !d.Changed {
				t.Fatal(d, e)
			}
		})
	}
}
