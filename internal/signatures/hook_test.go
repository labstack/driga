package signatures

import "testing"

func TestLoadAgentSkipsMCPPatterns(t *testing.T) {
	full := Builtin()
	for _, agent := range []string{"claude", "codex", "copilot"} {
		s, err := LoadAgent(agent)
		if err != nil {
			t.Fatal(err)
		}
		if len(s.mcp) != 0 || len(full.mcp) == 0 {
			t.Fatal("hook compiled unused MCP patterns, or full set lost them")
		}
		for _, cmd := range []string{agent, agent + " --help"} {
			got, ok := s.Match(cmd, "")
			want, found := full.Match(cmd, "")
			if ok != found || got.Name != want.Name || got.Role != want.Role {
				t.Fatal(cmd, got, want)
			}
		}
	}
}
