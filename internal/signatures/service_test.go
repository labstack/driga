package signatures_test

import (
	"testing"

	"github.com/labstack/driga/internal/signatures"
)

func TestCodexAppServerDaemonSignature(t *testing.T) {
	set, err := signatures.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	const exe = "/Users/example/.codex/packages/app-server-daemon/releases/1.2.3/bin/codex"
	for _, tt := range []struct {
		cmd, name, role string
	}{
		{exe + " app-server --remote-control", "codex-app-server-daemon", "service"},
		{exe + " app-server daemon pid-update 123", "codex-app-server-daemon", "service"},
		{"codex app-server", "codex-app-server", "host"},
		{"/opt/bin/codex app-server", "codex-app-server", "host"},
		{"codex app-server --exe " + exe, "codex-app-server", "host"},
		{"echo " + exe + " app-server", "", ""},
		{exe + " exec task", "codex-exec", "headless"},
		{exe + " app-server-other", "codex-interactive", "interactive"},
		{"/Users/example/.codex/packages/app-server-daemon-other/bin/codex app-server", "codex-app-server", "host"},
	} {
		t.Run(tt.cmd, func(t *testing.T) {
			sig, ok := set.Match(tt.cmd, "")
			if ok != (tt.name != "") || sig.Name != tt.name || string(sig.Role) != tt.role {
				t.Fatalf("Match = %+v, %t; want %s / %s", sig, ok, tt.name, tt.role)
			}
		})
	}
}
