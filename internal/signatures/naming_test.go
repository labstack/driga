package signatures

import "testing"

func TestProcessNamePreservesVersions(t *testing.T) {
	s, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ exe, cmd, want string }{
		{"/Users/fixture/.local/share/claude/versions/2.1.304", "claude", "claude 2.1.304"},
		{"/Users/fixture/.local/share/claude/versions/2.1.303", "/Users/fixture/.local/share/claude/versions/2.1.303 --resume", "claude 2.1.303"},
		{"", "/Users/fixture/.local/share/claude/versions/2.1.304 --resume", "claude 2.1.304"},
		{"/opt/widget/releases/v3.2.1-rc.1", "", "widget v3.2.1-rc.1"},
		{"/opt/widget/3.2.1", "", "widget 3.2.1"},
		{"/bin/node", "node /opt/@github/copilot/npm-loader.js", "copilot"},
		{"/bin/python3.14", "", "python3.14"},
		{"/opt/bin/tool", "", "tool"},
		{"", "", "unknown"},
	} {
		if got := s.ProcessName(tc.cmd, tc.exe); got != tc.want {
			t.Errorf("%s %s: %q want %q", tc.exe, tc.cmd, got, tc.want)
		}
	}
	sig, ok := s.Match("/Users/fixture/.local/share/claude/versions/2.1.304 --resume", "")
	if !ok || sig.Agent != "claude" || sig.Role != RoleInteractive {
		t.Fatalf("versioned signature: %+v %t", sig, ok)
	}
	if sig, ok := s.Match("2.1.304 --resume", "/home/fixture/.local/share/claude/versions/2.1.304"); !ok || sig.Agent != "claude" {
		t.Fatalf("basename argv0: %+v %t", sig, ok)
	}
}

func TestMCPPatternsAreReportOnly(t *testing.T) {
	s, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"java -cp /opt/maestro/lib/* maestro.cli.AppKt mcp", "node /opt/codegraph/dist/index.js"} {
		if _, ok := s.MCPName(cmd, ""); !ok {
			t.Fatal("missing MCP evidence", cmd)
		}
		if sig, ok := s.Match(cmd, ""); ok {
			t.Fatalf("MCP report added orphan classification: %+v", sig)
		}
	}
}
