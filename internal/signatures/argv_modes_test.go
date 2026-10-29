package signatures_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/signatures"
)

func TestCodexArgvModes(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		want string
	}{
		{"escaped sandbox in TUI config", []string{"codex", "-c", `label="say \" sandbox words"`}, "codex-interactive"},
		{"escaped app-server in TUI config", []string{"codex", "-c", `label="say \" app-server words"`}, "codex-interactive"},
		{"escaped sandbox before exec", []string{"codex", "-c", `label="say \" sandbox words"`, "exec", "task"}, "codex-exec"},
		{"escaped app-server before exec", []string{"codex", "-c", `label="say \" app-server words"`, "exec", "task"}, "codex-exec"},
		{"array app-server", []string{"codex", "-c", `sandbox_workspace_write.writable_roots=["/tmp/a", "/tmp/b"]`, "app-server"}, "codex-app-server"},
		{"array sandbox", []string{"codex", "--config", `sandbox_workspace_write.writable_roots=["/tmp/a", "/tmp/b"]`, "sandbox"}, "codex-sandbox"},
		{"short equals", []string{"codex", `-c=sandbox_workspace_write.writable_roots=["/tmp/a", "/tmp/b"]`, "app-server"}, "codex-app-server"},
		{"long equals", []string{"codex", `--config=label="a \" sandbox words"`, "app-server"}, "codex-app-server"},
		{"repeated configs", []string{"codex", "-c", `label="sandbox app-server"`, "--config=true=false", "-c=third=true", "sandbox"}, "codex-sandbox"},
		{"exec prompt", []string{"codex", "exec", "sandbox app-server"}, "codex-exec"},
		{"TUI prompt", []string{"codex", "sandbox app-server"}, "codex-interactive"},
		{"path argument", []string{"codex", "--resume", "/tmp/app-server/sandbox"}, "codex-interactive"},
		{"terminator", []string{"codex", "--", "app-server"}, "codex-interactive"},
		{"missing config", []string{"codex", "-c"}, "codex-interactive"},
		{"empty config value", []string{"codex", "-c", "", "app-server"}, "codex-app-server"},
		{"Windows path and array", []string{`C:\Program Files\Codex\CODEX.EXE`, "-c", `sandbox_workspace_write.writable_roots=["C:\\Temp\\a", "C:\\Temp\\b"]`, "app-server"}, "codex-app-server"},
		{"Windows TUI prompt", []string{`C:\Program Files\Codex\codex.exe`, "sandbox app-server"}, "codex-interactive"},
		{"spaced Unix executable", []string{"/opt/Agent Tools/codex", "-c=one=true", "sandbox"}, "codex-sandbox"},
		{"versioned executable", []string{"/opt/codex/versions/1.2.3", "-c=one=true", "app-server"}, "codex-app-server"},
		{"mode in another executable", []string{"echo", "codex", "sandbox"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]string(nil), tc.argv...)
			sig, ok := signatures.Builtin().Match(strings.Join(tc.argv, " "), tc.argv[0], tc.argv)
			if ok != (tc.want != "") || sig.Name != tc.want {
				t.Errorf("Match argv %#v = %q/%t, want %q", tc.argv, sig.Name, ok, tc.want)
			}
			if !reflect.DeepEqual(before, tc.argv) {
				t.Fatal("matcher changed the platform argv")
			}
		})
	}
}

func TestClaudeArgvModes(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		want string
	}{
		{[]string{"claude", "daemon", "run"}, "claude-daemon"},
		{[]string{"claude", "daemon run"}, "claude-interactive"},
		{[]string{"claude", "bg-spare --bg-spare"}, "claude-interactive"},
		{[]string{"claude", "--chrome-native-host prompt"}, "claude-interactive"},
		{[]string{"claude", "bg-pty-host", "/tmp/bg-spare"}, "claude-background-helper"},
		{[]string{"claude", "--bg-spare=/tmp/socket"}, "claude-background-helper"},
		{[]string{"claude", "--chrome-native-host"}, "claude-chrome-native-host"},
		{[]string{"claude", "--", "--chrome-native-host"}, "claude-interactive"},
		{[]string{"claude", "-p", "daemon run bg-pty-host bg-spare --chrome-native-host"}, "claude-headless"},
		{[]string{`C:\Program Files\Claude\CLAUDE.EXE`, "daemon", "run"}, "claude-daemon"},
		{[]string{"/opt/claude/versions/2.1.314", "bg-spare"}, "claude-background-helper"},
	} {
		t.Run(strings.Join(tc.argv, "|"), func(t *testing.T) {
			sig, ok := signatures.Builtin().Match(strings.Join(tc.argv, " "), tc.argv[0], tc.argv)
			if !ok || sig.Name != tc.want {
				t.Errorf("Match argv %#v = %q/%t, want %q", tc.argv, sig.Name, ok, tc.want)
			}
		})
	}
}

func TestInfrastructureModesRequireArgv(t *testing.T) {
	for _, cmd := range []string{"codex app-server", "codex sandbox", "claude daemon run", "claude --chrome-native-host"} {
		sig, ok := signatures.Builtin().Match(cmd, "", nil)
		if !ok || sig.Role != signatures.RoleInteractive {
			t.Errorf("unavailable argv must not infer infrastructure from %q: %+v/%t", cmd, sig, ok)
		}
	}
}
