package signatures

import (
	"reflect"
	"testing"
)

func TestWindowsWords(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		want []string
	}{
		{`"C:\Program Files\agent.exe" "子 folder" ""`, []string{`C:\Program Files\agent.exe`, "子 folder", ""}},
		{`agent.exe C:\work\ --flag`, []string{"agent.exe", `C:\work\`, "--flag"}},
		{`agent.exe "a\"b"`, []string{"agent.exe", `a"b`}},
	} {
		if got := windowsWords(tc.cmd); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q: %#v", tc.cmd, got)
		}
	}
}

func TestWindowsExecutableSignatures(t *testing.T) {
	s, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		cmd, exe, agent string
		role            Role
	}{
		{`"C:\Program Files\Claude\claude.exe" --print hello`, `C:\Program Files\Claude\claude.exe`, "claude", RoleHeadless},
		{`C:\Tools\CODEX.EXE app-server`, `C:\Tools\CODEX.EXE`, "codex", RoleHost},
		{`"C:\Program Files\nodejs\node.exe" "C:\Agent Tools\node_modules\@github\copilot\npm-loader.js"`, `C:\Program Files\nodejs\node.exe`, "copilot", RoleInteractive},
		{`node.exe C:\Tools\@google\gemini-cli\dist\index.js`, `C:\Tools\node.exe`, "gemini", RoleInteractive},
		{`C:\Tools\vim.exe C:\Tools\claude.exe`, `C:\Tools\vim.exe`, "", ""},
	} {
		got, ok := s.Match(tc.cmd, tc.exe, windowsWords(tc.cmd))
		if ok != (tc.agent != "") || got.Agent != tc.agent || got.Role != tc.role {
			t.Fatalf("%s: %+v %v", tc.cmd, got, ok)
		}
	}
}
