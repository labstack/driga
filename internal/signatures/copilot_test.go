package signatures

import "testing"

func TestCopilotSignatures(t *testing.T) {
	s, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ cmd, role string }{{"copilot", "interactive"}, {"/usr/local/bin/copilot --resume abc", "interactive"}, {"copilot -p hello", "headless"}, {"copilot --prompt=hello", "headless"}, {"copilot --model x --prompt hello", "headless"}, {"node /opt/node_modules/@github/copilot/npm-loader.js", "interactive"}, {"/usr/bin/node /opt/@github/copilot/npm-loader.js --prompt hello", "headless"}, {"echo /opt/@github/copilot/npm-loader.js", ""}, {"echo copilot", ""}, {"copilot-helper", ""}} {
		sig, ok := s.Match(tc.cmd, "", nil)
		if tc.role == "" {
			if ok && sig.Agent == "copilot" {
				t.Fatal(tc, sig)
			}
			continue
		}
		if !ok || sig.Agent != "copilot" || string(sig.Role) != tc.role {
			t.Fatal(tc, sig, ok)
		}
	}
}
