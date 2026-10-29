package signatures_test

import (
	"github.com/kballard/go-shellquote"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/signatures"
)

func TestAgentHelperCommandModes(t *testing.T) {
	set := signatures.Builtin()
	for _, tc := range []struct {
		cmd, name string
		role      signatures.Role
	}{
		{"/Users/fixture/.local/bin/claude --chrome-native-host", "claude-chrome-native-host", signatures.RoleHelper},
		{"/opt/claude/versions/2.1.314 --chrome-native-host", "claude-chrome-native-host", signatures.RoleHelper},
		{`"C:\Program Files\Claude\claude.exe" --chrome-native-host`, "claude-chrome-native-host", signatures.RoleHelper},
		{"codex sandbox -c default_permissions=node_repl", "codex-sandbox", signatures.RoleHelper},
		{"/opt/bin/codex -c features.code_mode_host=true app-server --analytics-default-enabled -c plugins.example.enabled=true", "codex-app-server", signatures.RoleHost},
		{"codex -c one=true --config two=false app-server --listen stdio://", "codex-app-server", signatures.RoleHost},
		{"codex --config=one=true sandbox", "codex-sandbox", signatures.RoleHelper},
		{"codex app-server", "codex-app-server", signatures.RoleHost},
		{"/Applications/Fixture.app/Contents/MacOS/codex app-server", "codex-app-server", signatures.RoleHost},
		{`codex -c 'label="some words"' app-server`, "codex-app-server", signatures.RoleHost},
		{`codex -c label="some words" sandbox`, "codex-sandbox", signatures.RoleHelper},
		{`"C:\Program Files\Codex\codex.exe" -c mode=true app-server`, "codex-app-server", signatures.RoleHost},
		{"claude --chrome-native-host-other", "claude-interactive", signatures.RoleInteractive},
		{`claude -p "explain --chrome-native-host"`, "claude-headless", signatures.RoleHeadless},
		{"claude -- --chrome-native-host", "claude-interactive", signatures.RoleInteractive},
		{"codex sandbox-other", "codex-interactive", signatures.RoleInteractive},
		{"codex app-server-other", "codex-interactive", signatures.RoleInteractive},
		{"codex -c app-server sandbox-other", "codex-interactive", signatures.RoleInteractive},
		{"codex -- sandbox", "codex-interactive", signatures.RoleInteractive},
		{"codex exec echo sandbox app-server", "codex-exec", signatures.RoleHeadless},
		{`codex -c label="some app-server words" exec task`, "codex-exec", signatures.RoleHeadless},
		{`codex -c 'label="some sandbox words"' exec task`, "codex-exec", signatures.RoleHeadless},
		{"codex --resume /tmp/app-server/sandbox", "codex-interactive", signatures.RoleInteractive},
		{"echo claude --chrome-native-host", "", ""},
		{"echo codex sandbox", "", ""},
	} {
		t.Run(tc.cmd, func(t *testing.T) {
			sig, ok := set.Match(tc.cmd, "", commandFixtureArgv(t, tc.cmd))
			if ok != (tc.name != "") || sig.Name != tc.name || sig.Role != tc.role {
				t.Errorf("Match = %q/%q, %t; want %q/%q", sig.Name, sig.Role, ok, tc.name, tc.role)
			}
		})
	}
}

// commandFixtureArgv decodes only the hand-authored shell examples in these
// legacy tables. Regression tests supply actual argv elements directly.
func commandFixtureArgv(t *testing.T, cmd string) []string {
	t.Helper()
	if strings.HasPrefix(cmd, `"C:\`) {
		exe, rest, _ := strings.Cut(cmd[1:], `"`)
		return append([]string{exe}, strings.Fields(rest)...)
	}
	args, err := shellquote.Split(cmd)
	if err != nil {
		t.Fatal(err)
	}
	return args
}
