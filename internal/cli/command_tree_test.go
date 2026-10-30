package cli

import (
	"bytes"
	"errors"
	"github.com/alecthomas/kong"
	"github.com/labstack/driga/internal/actions"
	"io"
	"strings"
	"testing"
)

func parseCommand(t *testing.T, args ...string) (*Root, *kong.Context, error) {
	t.Helper()
	root := new(Root)
	parser, err := kong.New(root, kong.Bind(&root.Globals), kong.Vars{"version": "test"}, kong.Writers(io.Discard, io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := parser.Parse(args)
	return root, ctx, err
}
func TestCommandTree(t *testing.T) {
	paths := []string{
		"", "summary", "flow", "flow 12", "agents", "agents list", "agents show 12", "agents jump 12", "agents kill 12", "projects", "projects list", "projects show app", "projects summarize app other", "projects summarize --all", "inbox", "inbox list", "inbox ack item", "inbox ack --all", "inbox jump item", "orphans", "orphans list", "orphans kill item", "orphans kill --all", "machines", "machines list", "machines show", "machines show local", "machines reset-id", "cleanup", "cleanup list", "cleanup remove item", "cleanup remove --all --kind worktree", "top", "app", "app --cpu-profile app.pprof --profile-for 1m", "dev serve --cpu-profile app.pprof --profile-for 1m", "ask what needs me", "mcp", "doctor", "init", "config path", "config show", "config edit", "service run", "service run --serve --listen 127.0.0.1:7411", "service run --scheduled --service-data-dir fixture --service-env DRIGA_CAPTURE_TEXT=titles", "service install", "service start", "service stop", "service restart", "service status", "service logs -f", "service uninstall", "hooks install", "hooks install claude", "hooks uninstall codex", "hooks status", "hooks status all", "fleet peers list", "fleet peers add host:7411 --device-id device -y", "fleet peers remove peer", "fleet viewers list", "fleet viewers revoke viewer", "fleet enroll --allow-viewer viewer --allow-ack -y", "fleet rekey", "fleet id", "fleet status", "version", "hook claude Stop", "feed tail --after-seq 1 -f", "dev agy-schema", "mcp --chat", "mcp --snapshot",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			if _, _, err := parseCommand(t, strings.Fields(path)...); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, tab := range []string{"flow", "projects", "machines"} {
		if _, _, err := parseCommand(t, "top", "--tab", tab); err != nil {
			t.Fatal(tab, err)
		}
	}
	for _, tab := range []string{"home", "agents", "inbox", "machine", "chat"} {
		if _, _, err := parseCommand(t, "top", "--tab", tab); err == nil {
			t.Fatal("removed tab accepted:", tab)
		}
	}
}
func TestRemovedSpellingsAndUnsupportedFlags(t *testing.T) {
	paths := []string{"kill item", "agent 12", "where app", "summarize app", "jump item", "serve", "machine show", "machine reset-id", "fleet add host", "fleet remove peer", "fleet revoke viewer", "fleet peers", "fleet viewers", "fleet peers peer", "fleet viewers viewer", "fleet enroll --allow id", "fleet enroll --ack", "fleet peers add host --id id", "hooks install --agent claude", "hooks status --yes", "hooks status --dry-run", "hooks status --traces", "service start -f", "agents kill 12 --approved", "agents jump 12 --machine remote", "machines show --machine local", "projects summarize app --project other", "cleanup item", "cleanup remove item --why", "cleanup list --yes", "version --machine local", "doctor --snapshot", "mcp --json", "web", "app --json", "agents --theme mocha", "agents show 12 --since 1h", "flow --watch 2s", "activity", "conflicts", "agents --watch 2s", "projects show app --project other", "projects show app --since 1h", "inbox ack item --watch 2s", "projects --theme dark"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			if _, _, err := parseCommand(t, strings.Fields(path)...); err == nil {
				t.Fatal("accepted removed or unsupported spelling")
			}
		})
	}
}
func TestScopedOptionsAndDefaults(t *testing.T) {
	for _, path := range []string{"summary --json --machine local", "agents --json --machine local", "agents list --json --machine local"} {
		root, _, err := parseCommand(t, strings.Fields(path)...)
		if err != nil || !root.JSON || root.Machine != "local" {
			t.Fatal(path, root.Globals, err)
		}
	}
	for _, tc := range []struct{ path, command string }{{"", "summary"}, {"agents", "agents list"}, {"projects", "projects list"}, {"inbox", "inbox list"}, {"orphans", "orphans list"}, {"machines", "machines list"}, {"cleanup", "cleanup list"}} {
		_, ctx, err := parseCommand(t, strings.Fields(tc.path)...)
		if err != nil || ctx.Command() != tc.command {
			t.Fatal(tc.path, ctx, err)
		}
	}
}
func TestCommandConfirmationNonTerminal(t *testing.T) {
	readEnv(t)
	paths := []string{"agents kill 12", "agents jump 12", "orphans kill item", "machines reset-id", "fleet rekey", "fleet peers remove peer", "fleet viewers revoke viewer", "hooks install claude", "init", "cleanup remove item"}
	for _, path := range paths {
		var out, diagnostic bytes.Buffer
		code := run(strings.Fields(path), "test", strings.NewReader("y\n"), &out, &diagnostic)
		if code != 2 || !strings.Contains(diagnostic.String(), "not applied; pass --yes to apply without asking") {
			t.Errorf("%s: code=%d %s", path, code, &diagnostic)
		}
	}
	for _, path := range []string{"fleet enroll", "fleet enroll --yes", "fleet peers add host", "fleet peers add host --yes"} {
		var out, diagnostic bytes.Buffer
		if code := run(strings.Fields(path), "test", strings.NewReader("y\n"), &out, &diagnostic); code != 2 {
			t.Fatal(path, code, &diagnostic)
		}
	}
}
func TestConfirmationConsentAndRefusal(t *testing.T) {
	for _, yes := range []bool{false, true} {
		var out bytes.Buffer
		g := &Globals{Stdin: strings.NewReader("y\n"), Stdout: &out}
		err := requireConfirmation(g, yes, "Apply?")
		if yes {
			if err != nil || out.Len() != 0 {
				t.Fatal(err, &out)
			}
		} else {
			var code interface{ ExitCode() int }
			if !errors.As(err, &code) || code.ExitCode() != 2 {
				t.Fatal(err)
			}
		}
	}
	for _, answer := range []string{"y\n", "yes\n", "n\n", "\n", ""} {
		var out bytes.Buffer
		err := requireConfirmation(&Globals{inputTerminal: true, Stdin: strings.NewReader(answer), Stdout: &out}, false, "Apply?")
		accepted := answer == "y\n" || answer == "yes\n"
		if (err == nil) != accepted || !strings.Contains(out.String(), "Apply? [y/N]") {
			t.Fatal(answer, err, &out)
		}
		if !accepted && !errors.Is(err, actions.ErrRefused) {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"agents kill 12", "agents jump 12", "orphans kill item", "machines reset-id", "fleet rekey", "fleet peers remove peer", "fleet viewers revoke viewer", "hooks install claude", "hooks uninstall claude", "init", "cleanup remove item"} {
		for _, flag := range []string{"--yes", "-y"} {
			if _, _, err := parseCommand(t, append(strings.Fields(path), flag)...); err != nil {
				t.Fatal(path, flag, err)
			}
		}
	}
}
func TestExitCodeContracts(t *testing.T) {
	readEnv(t)
	readDB(t, nil)
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"version"}, 0}, {[]string{"--version"}, 0}, {[]string{"agents", "show", "999999999"}, 1}, {[]string{"agents", "kill", "999999999", "--yes"}, 1},
		{[]string{"agents", "show", "0"}, 2}, {[]string{"flow", "0"}, 2}, {[]string{"flow", "--minutes", "0"}, 2}, {[]string{"inbox", "ack"}, 2}, {[]string{"orphans", "kill", "--yes"}, 2}, {[]string{"projects", "summarize"}, 2}, {[]string{"projects", "summarize", "app", "--all"}, 2}, {[]string{"machines", "show", "all"}, 2}, {[]string{"service", "run", "--serve", "--once"}, 2}, {[]string{"cleanup", "remove", "--all"}, 2}, {[]string{"cleanup", "remove", "--all", "--kind", "history"}, 2}, {[]string{"hook"}, 0},
	} {
		var out, diagnostic bytes.Buffer
		if got := run(tc.args, "test", nil, &out, &diagnostic); got != tc.code {
			t.Errorf("%v: got %d want %d: %s", tc.args, got, tc.code, &diagnostic)
		}
	}
}
