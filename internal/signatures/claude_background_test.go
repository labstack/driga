package signatures_test

import (
	"testing"

	"github.com/labstack/driga/internal/signatures"
)

func TestClaudeBackgroundModes(t *testing.T) {
	set := signatures.Builtin()
	for _, tc := range []struct {
		cmd, exe, name string
	}{
		{"claude daemon run --json-path /arbitrary/state.json", "", "claude-daemon"},
		{"/opt/bin/claude bg-pty-host --bg-pty-host /arbitrary/pty.sock 200 50 -- claude -p task", "", "claude-background-helper"},
		{"claude bg-spare --bg-spare /arbitrary/claim.sock", "", "claude-background-helper"},
		{"claude --bg-spare /arbitrary/claim.sock", "", "claude-background-helper"},
		{"claude --bg-pty-host /arbitrary/pty.sock", "", "claude-background-helper"},
		{"/Users/fixture/.local/share/claude/versions/2.1.314 bg-spare --bg-spare /socket", "", "claude-background-helper"},
		{"2.1.314 daemon run", "/Users/fixture/.local/share/claude/versions/2.1.314", "claude-daemon"},
		{`"C:\Program Files\Claude\claude.exe" bg-spare --bg-spare C:\Temp\claim.sock`, "", "claude-background-helper"},
		{"claude daemon status", "", "claude-interactive"},
		{"claude bg-spare-other", "", "claude-interactive"},
		{"claude --bg-spare-other /socket", "", "claude-interactive"},
		{"claude --resume /tmp/cc-daemon-501/spare/session", "", "claude-interactive"},
		{`claude -p "explain bg-spare --bg-spare and daemon run"`, "", "claude-headless"},
		{"claude -- --bg-spare", "", "claude-interactive"},
		{"echo claude daemon run", "", ""},
	} {
		t.Run(tc.cmd, func(t *testing.T) {
			sig, ok := set.Match(tc.cmd, tc.exe, commandFixtureArgv(t, tc.cmd))
			if ok != (tc.name != "") || sig.Name != tc.name {
				t.Fatalf("Match = %q, %t; want %q", sig.Name, ok, tc.name)
			}
			if tc.name == "claude-daemon" && sig.Role != signatures.RoleService || tc.name == "claude-background-helper" && sig.Role != signatures.RoleHelper {
				t.Fatalf("wrong infrastructure role: %+v", sig)
			}
		})
	}
}
