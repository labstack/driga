package signatures_test

import (
	"strings"
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
		{exe + ` -c 'label="some app-server words"' app-server`, "codex-app-server-daemon", "service"},
		{"codex app-server", "codex-app-server", "host"},
		{"/opt/bin/codex app-server", "codex-app-server", "host"},
		{"codex app-server --exe " + exe, "codex-app-server", "host"},
		{"echo " + exe + " app-server", "", ""},
		{exe + " exec task", "codex-exec", "headless"},
		{exe + " app-server-other", "codex-interactive", "interactive"},
		{"/Users/example/.codex/packages/app-server-daemon-other/bin/codex app-server", "codex-app-server", "host"},
	} {
		t.Run(tt.cmd, func(t *testing.T) {
			sig, ok := set.Match(tt.cmd, "", commandFixtureArgv(t, tt.cmd))
			if ok != (tt.name != "") || sig.Name != tt.name || string(sig.Role) != tt.role {
				t.Fatalf("Match = %+v, %t; want %s / %s", sig, ok, tt.name, tt.role)
			}
		})
	}
}

func TestCodexDaemonArgvBoundaries(t *testing.T) {
	const exe = "/Users/example/.codex/packages/app-server-daemon/releases/1.2.3/bin/codex"
	for _, tc := range []struct {
		name, exe, want string
		argv            []string
	}{
		{"resolved exe", exe, "codex-app-server-daemon", []string{"codex", "app-server"}},
		{"array override", exe, "codex-app-server-daemon", []string{exe, "-c", `roots=["/tmp/a", "/tmp/b"]`, "app-server"}},
		{"one prompt element", exe, "codex-interactive", []string{exe, "app-server prompt"}},
		{"mode in config", exe, "codex-interactive", []string{exe, "-c", "label=true app-server"}},
		{"exec mode", exe, "codex-exec", []string{exe, "exec", "app-server"}},
		{"terminator", exe, "codex-interactive", []string{exe, "--", "app-server"}},
		{"path argument", "codex", "codex-app-server", []string{"codex", "app-server", "--exe", exe}},
		{"Windows daemon", `C:\Users\example\.codex\packages\app-server-daemon\bin\codex.exe`, "codex-app-server-daemon", []string{`C:\Users\example\.codex\packages\app-server-daemon\bin\codex.exe`, "app-server"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sig, ok := signatures.Builtin().Match(strings.Join(tc.argv, " "), tc.exe, tc.argv)
			if !ok || sig.Name != tc.want {
				t.Fatalf("Match = %+v, %t; want %s", sig, ok, tc.want)
			}
		})
	}
	if sig, _ := signatures.Builtin().Match(exe+" app-server", exe, nil); sig.Role == signatures.RoleService {
		t.Fatalf("daemon command mode inferred without argv: %+v", sig)
	}
}
