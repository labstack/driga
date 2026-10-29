package signatures_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/signatures"
)

func TestMatch(t *testing.T) {
	set, err := signatures.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, cmdline, exe, agent string
		role                      signatures.Role
		appBundle, wantMatch      bool
	}{
		{name: "claude interactive", cmdline: "claude", agent: "claude", role: signatures.RoleInteractive, wantMatch: true},
		{name: "claude absolute executable", cmdline: "/usr/local/bin/claude --resume", agent: "claude", role: signatures.RoleInteractive, wantMatch: true},
		{name: "claude print short", cmdline: `claude -p "x"`, agent: "claude", role: signatures.RoleHeadless, wantMatch: true},
		{name: "claude print long", cmdline: `claude --print "x"`, agent: "claude", role: signatures.RoleHeadless, wantMatch: true},
		{name: "claude print after flags", cmdline: `claude --model example -p "x"`, agent: "claude", role: signatures.RoleHeadless, wantMatch: true},
		{name: "claude mcp", cmdline: "claude mcp serve", agent: "claude", role: signatures.RoleHelper, wantMatch: true},
		{name: "claude absolute mcp", cmdline: "/opt/bin/claude mcp serve --verbose", agent: "claude", role: signatures.RoleHelper, wantMatch: true},
		{name: "codex interactive", cmdline: "codex", agent: "codex", role: signatures.RoleInteractive, wantMatch: true},
		{name: "codex absolute executable", cmdline: "/opt/bin/codex --resume", agent: "codex", role: signatures.RoleInteractive, wantMatch: true},
		{name: "codex exec", cmdline: "codex exec -s workspace-write …", agent: "codex", role: signatures.RoleHeadless, wantMatch: true},
		{name: "codex absolute exec", cmdline: "/opt/bin/codex exec task", agent: "codex", role: signatures.RoleHeadless, wantMatch: true},
		{name: "codex host", cmdline: "codex app-server --listen unix://", agent: "codex", role: signatures.RoleHost, wantMatch: true},
		{name: "codex broker", cmdline: "node /opt/plugin/app-server-broker.mjs serve", agent: "codex", role: signatures.RoleHelper, wantMatch: true},
		{name: "codex absolute node", cmdline: "/usr/local/bin/node /opt/plugin/app-server-broker.mjs serve", agent: "codex", role: signatures.RoleHelper, wantMatch: true},
		{name: "codex direct broker", cmdline: "/opt/plugin/app-server-broker.mjs serve", agent: "codex", role: signatures.RoleHelper, wantMatch: true},
		{name: "npx mcp", cmdline: "npx -y some-mcp-server", role: signatures.RoleHelper, wantMatch: true},
		{name: "uvx mcp", cmdline: "uvx some-mcp", role: signatures.RoleHelper, wantMatch: true},
		{name: "bunx mcp", cmdline: "bunx some-mcp-server --stdio", role: signatures.RoleHelper, wantMatch: true},
		{name: "pipx mcp", cmdline: "pipx run some-mcp-server", role: signatures.RoleHelper, wantMatch: true},
		{name: "absolute launcher", cmdline: "/usr/bin/npx -y @example/mcp-server", role: signatures.RoleHelper, wantMatch: true},
		{name: "system app", cmdline: "/Applications/X.app/Contents/MacOS/X", role: signatures.RoleApp, appBundle: true, wantMatch: true},
		{name: "different app executable", cmdline: "/Applications/Example.app/Contents/MacOS/runner --flag", role: signatures.RoleApp, appBundle: true, wantMatch: true},
		{name: "app with spaces", cmdline: "/Applications/Example Editor.app/Contents/MacOS/editor --flag", role: signatures.RoleApp, appBundle: true, wantMatch: true},
		{name: "quoted app", cmdline: `"/Applications/Example Editor.app/Contents/MacOS/editor" --flag`, role: signatures.RoleApp, appBundle: true, wantMatch: true},
		{name: "home app", cmdline: "~/Applications/X.app/Contents/MacOS/X", role: signatures.RoleApp, appBundle: true, wantMatch: true},
		{name: "expanded home app", cmdline: "/Users/example/Applications/X.app/Contents/MacOS/X", role: signatures.RoleApp, appBundle: true, wantMatch: true},
		{name: "exe fallback agent", exe: "/opt/bin/codex", agent: "codex", role: signatures.RoleInteractive, wantMatch: true},
		{name: "exe fallback app", exe: "/Applications/X.app/Contents/MacOS/X", role: signatures.RoleApp, appBundle: true, wantMatch: true},
		{name: "cmdline takes precedence", cmdline: "claude -p x", exe: "codex", agent: "claude", role: signatures.RoleHeadless, wantMatch: true},
		{name: "unrelated executable", cmdline: "vim main.go"},
		{name: "agent in filename", cmdline: "vim claude.md"},
		{name: "agent argument", cmdline: "grep codex"},
		{name: "agent path argument", cmdline: "vim /usr/bin/claude"},
		{name: "headless argument", cmdline: "echo codex exec task"},
		{name: "mcp argument", cmdline: "echo claude mcp serve"},
		{name: "broker argument", cmdline: "vim /opt/plugin/app-server-broker.mjs"},
		{name: "launcher argument", cmdline: "echo npx some-mcp-server"},
		{name: "app argument", cmdline: "vim /Applications/X.app/Contents/MacOS/X"},
		{name: "absolute executable app argument", cmdline: "/usr/bin/vim /Applications/X.app/Contents/MacOS/X"},
		{name: "agent suffix", cmdline: "codex-other"},
		{name: "agent file", cmdline: "claude.md"},
		{name: "npx without mcp", cmdline: "npx -y some-server"},
		{name: "uvx without mcp", cmdline: "uvx some-server"},
		{name: "bunx without mcp", cmdline: "bunx some-server"},
		{name: "pipx without run", cmdline: "pipx list some-mcp"},
		{name: "app resources helper", cmdline: "/Applications/X.app/Contents/Resources/helper"},
		{name: "nonempty cmdline prevents fallback", cmdline: "grep codex", exe: "codex"},
		{name: "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var argv []string
			if strings.Contains(tt.cmdline, "claude") || strings.Contains(tt.cmdline, "codex") {
				argv = commandFixtureArgv(t, tt.cmdline)
			}
			got, ok := set.Match(tt.cmdline, tt.exe, argv)
			if ok != tt.wantMatch {
				t.Fatalf("Match(%q, %q) = %+v, %v; want match %v", tt.cmdline, tt.exe, got, ok, tt.wantMatch)
			}
			if ok && (got.Agent != tt.agent || got.Role != tt.role || got.AppBundle != tt.appBundle) {
				t.Errorf("Match = %+v; want agent %q, role %q, AppBundle %v", got, tt.agent, tt.role, tt.appBundle)
			}
		})
	}
}

func TestExtraSignaturesPrepend(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.toml")
	second := filepath.Join(dir, "second.toml")
	writeFile(t, first, `
[[signature]]
name = "custom-first"
agent = "custom"
role = "helper"
match = ['^unused$', '^codex( |$)']
app_bundle = true

[[signature]]
name = "custom-later"
role = "host"
match = ['^codex( |$)']
`)
	writeFile(t, second, `
[[signature]]
name = "second-file"
role = "headless"
match = ['^codex( |$)']
`)
	set, err := signatures.Load([]string{first, second})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := set.Match("codex", "", nil)
	if !ok || got.Name != "custom-first" || got.Agent != "custom" || got.Role != signatures.RoleHelper || !got.AppBundle || len(got.Match) != 2 {
		t.Fatalf("extra signatures did not take precedence or retain fields: %+v, %v", got, ok)
	}
	if got, ok := set.Match("claude", "", nil); !ok || got.Agent != "claude" {
		t.Fatalf("embedded signatures missing after extras: %+v, %v", got, ok)
	}
}

func TestBadRegexFailsLoad(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.toml")
	writeFile(t, valid, `
[[signature]]
name = "valid"
role = "helper"
match = ['.*']
`)
	for _, patterns := range []string{`['[']`, `['.*', '[']`} {
		t.Run(patterns, func(t *testing.T) {
			bad := filepath.Join(dir, "bad.toml")
			writeFile(t, bad, "[[signature]]\nname = 'broken-signature'\nrole = 'helper'\nmatch = "+patterns+"\n")
			set, err := signatures.Load([]string{valid, bad})
			if err == nil || set != nil {
				t.Fatalf("Load bad regex = %v, %v; want nil set and error", set, err)
			}
			if !strings.Contains(err.Error(), bad) || !strings.Contains(err.Error(), "broken-signature") {
				t.Fatalf("error must name file and signature: %v", err)
			}
		})
	}
}

func TestBundlePath(t *testing.T) {
	tests := []struct{ input, want string }{
		{"/Applications/X.app/Contents/MacOS/X", "/Applications/X.app"},
		{"/Applications/Example Editor.app/Contents/MacOS/editor --flag", "/Applications/Example Editor.app"},
		{`"/Applications/Example Editor.app/Contents/MacOS/editor" --flag`, "/Applications/Example Editor.app"},
		{"'~/Applications/X.app/Contents/MacOS/X' --flag", "~/Applications/X.app"},
		{"/Users/example/Applications/X.app/Contents/MacOS/X", "/Users/example/Applications/X.app"},
		{"/Applications/X.app/Contents/Resources/helper --flag", "/Applications/X.app"},
		{"vim /Applications/X.app/Contents/MacOS/X", ""},
		{"/usr/bin/vim /Applications/X.app/Contents/MacOS/X", ""},
		{"/Applications/X.application/Contents/MacOS/X", ""},
		{"/Applications/X.app-other/Contents/MacOS/X", ""},
		{"codex", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := signatures.BundlePath(tt.input)
			if got != tt.want || ok != (tt.want != "") {
				t.Errorf("BundlePath(%q) = %q, %v; want %q, %v", tt.input, got, ok, tt.want, tt.want != "")
			}
		})
	}
}

func TestLoadErrors(t *testing.T) {
	dir := t.TempDir()
	t.Run("missing file", func(t *testing.T) {
		path := filepath.Join(dir, "missing.toml")
		if _, err := signatures.Load([]string{path}); err == nil || !strings.Contains(err.Error(), path) {
			t.Fatalf("expected error naming missing file, got %v", err)
		}
	})
	t.Run("malformed TOML", func(t *testing.T) {
		path := filepath.Join(dir, "malformed.toml")
		writeFile(t, path, "[[signature]\n")
		if _, err := signatures.Load([]string{path}); err == nil || !strings.Contains(err.Error(), path) {
			t.Fatalf("expected error naming malformed file, got %v", err)
		}
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
