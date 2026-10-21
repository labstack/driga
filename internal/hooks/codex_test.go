package hooks_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/labstack/driga/internal/cli"
	"github.com/labstack/driga/internal/hooks"
	"github.com/pelletier/go-toml/v2"
)

func TestCodexInstallWhenUnset(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission-bit contract; Windows uses ACLs")
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	before := "# keep in backup\nmodel = 'example'\n[custom]\ncount = 9007199254740993\n"
	writeFixture(t, path, before)
	d, err := hooks.CodexInstall(path, "/opt/tools with spaces/driga", false, testNow)
	if err != nil || !d.Changed || d.Before != before || d.After != readFixture(t, path) {
		t.Fatalf("install = %+v, %v", d, err)
	}
	var got map[string]any
	if err := toml.Unmarshal([]byte(d.After), &got); err != nil {
		t.Fatal(err)
	}
	want := []any{"/opt/tools with spaces/driga", "hook", "codex", "notify"}
	if !reflect.DeepEqual(got["notify"], want) || got["model"] != "example" || got["custom"].(map[string]any)["count"] != int64(9007199254740993) {
		t.Fatalf("round trip = %v", got)
	}
	if readFixture(t, path+".driga-bak-1800000000") != before {
		t.Fatal("original not backed up")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0640 {
		t.Fatalf("mode = %v", info.Mode())
	}
	d, err = hooks.CodexInstall(path, "/new/driga", false, testNow.Add(time.Second))
	if err != nil || d.Changed || d.Before != d.After {
		t.Fatalf("repeat = %+v, %v", d, err)
	}
	backups, _ := filepath.Glob(path + ".driga-bak-*")
	if len(backups) != 1 {
		t.Fatal("no-op wrote backup")
	}
}

// Another tool's notify (for example Codex Computer Use) must not block the
// permission hook: the hook is a separate table, so install appends it and
// leaves the foreign notify bytes alone, and uninstall restores the original.
func TestCodexInstallKeepsForeignNotify(t *testing.T) {
	for _, value := range []string{`["x", "y"]`, `[]`, `"custom"`, `["/opt/driga", "hook", "codex", "different"]`} {
		t.Run(value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			before := "# preserve me\nnotify = " + value + "\n"
			writeFixture(t, path, before)
			d, err := hooks.CodexInstall(path, "/opt/driga", false, testNow)
			if err != nil || !d.Changed || !strings.HasPrefix(d.After, before) || !strings.Contains(d.After, "[[hooks.PermissionRequest]]") || readFixture(t, path) != d.After {
				t.Fatalf("install = %+v, %v", d, err)
			}
			if strings.Contains(d.After, "# driga notify added") {
				t.Fatal("foreign notify claimed as ours")
			}
			u, err := hooks.CodexUninstall(path, false)
			if err != nil || !u.Changed || readFixture(t, path) != before {
				t.Fatalf("uninstall = %+v, %v; file %q", u, err, readFixture(t, path))
			}
		})
	}
}

func TestCodexUninstallOnlyIfOurs(t *testing.T) {
	for _, ours := range []bool{true, false} {
		path := filepath.Join(t.TempDir(), "config.toml")
		before := "notify = ['x', 'y']\nmodel = 'example'\n"
		if ours {
			before = "notify = ['/old/driga', 'hook', 'codex', 'notify']\nmodel = 'example'\n"
		}
		writeFixture(t, path, before)
		d, err := hooks.CodexUninstall(path, true)
		if err != nil || d.Changed != ours || readFixture(t, path) != before {
			t.Fatalf("dry uninstall = %+v, %v", d, err)
		}
		d, err = hooks.CodexUninstall(path, false)
		if err != nil || d.Changed != ours || !strings.Contains(d.After, "model") || (ours && strings.Contains(d.After, "notify")) || (!ours && d.After != before) {
			t.Fatalf("uninstall = %+v, %v", d, err)
		}
		backups, _ := filepath.Glob(path + ".driga-bak-*")
		if ours && (len(backups) != 1 || readFixture(t, backups[0]) != before) {
			t.Fatal("uninstall missing backup")
		}
		if !ours && len(backups) != 0 {
			t.Fatal("unrelated notify backed up")
		}
	}
}

func TestCodexDryRunAndMissingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission-bit contract; Windows uses ACLs")
	}

	path := filepath.Join(t.TempDir(), ".codex", "config.toml")
	d, err := hooks.CodexInstall(path, "/opt/driga", true, testNow)
	if err != nil || !d.Changed {
		t.Fatalf("dry install = %+v, %v", d, err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("dry run created directory")
	}
	if _, err := hooks.CodexUninstall(path, false); err != nil {
		t.Fatal(err)
	}
	if _, err := hooks.CodexInstall(path, "/opt/driga", false, testNow); err != nil {
		t.Fatal(err)
	}
	file, _ := os.Stat(path)
	dir, _ := os.Stat(filepath.Dir(path))
	if file.Mode().Perm() != 0600 || dir.Mode().Perm() != 0700 {
		t.Fatalf("permissions: %v, %v", file.Mode(), dir.Mode())
	}
}

func runHooksCLI(t *testing.T, input string, args ...string) string {
	t.Helper()
	t.Setenv("COPILOT_HOME", filepath.Join(os.Getenv("HOME"), ".copilot"))
	// Installers probe agent CLIs (copilot --version); never reach real ones.
	t.Setenv("PATH", t.TempDir())
	var root struct {
		Hooks cli.HooksCmd `cmd:""`
	}
	var output bytes.Buffer
	g := &cli.Globals{Stdin: strings.NewReader(input), Stdout: &output, Stderr: &output}
	parser, err := kong.New(&root, kong.Bind(g), kong.Writers(&output, &output))
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := parser.Parse(append([]string{"hooks"}, args...))
	if err != nil {
		t.Fatal(err)
	}
	if err := ctx.Run(); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func TestHooksCLIConfirmationAndStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("CODEX_HOME", "")
	claude := filepath.Join(home, ".claude", "settings.json")
	codex := filepath.Join(home, ".codex", "config.toml")
	for _, args := range [][]string{{"install", "--dry-run"}, {"install"}, {"install"}} {
		out := runHooksCLI(t, "no\n", args...)
		if !strings.Contains(out, "+") || (len(args) > 1 && strings.Contains(out, "Apply?")) {
			t.Fatalf("preview = %q", out)
		}
		if _, err := os.Stat(filepath.Dir(claude)); !os.IsNotExist(err) {
			t.Fatal("unconfirmed install wrote files")
		}
		if _, err := os.Stat(filepath.Dir(codex)); !os.IsNotExist(err) {
			t.Fatal("unconfirmed install wrote Codex files")
		}
	}
	if out := runHooksCLI(t, "yes\n", "install"); !strings.Contains(out, "Apply? [y/N]") {
		t.Fatalf("missing prompt: %q", out)
	}
	claudeBefore, codexBefore := readFixture(t, claude), readFixture(t, codex)
	if out := runHooksCLI(t, "", "status"); !strings.Contains(out, "SessionStart") || !strings.Contains(out, "Codex: installed") {
		t.Fatalf("status = %q", out)
	}
	if out := runHooksCLI(t, "", "install"); strings.Contains(out, "Apply?") {
		t.Fatalf("no-op prompted: %q", out)
	}
	runHooksCLI(t, "", "uninstall", "--dry-run")
	if readFixture(t, claude) != claudeBefore || readFixture(t, codex) != codexBefore {
		t.Fatal("dry uninstall wrote files")
	}
	runHooksCLI(t, "Y\n", "uninstall", "--agent", "claude")
	if readFixture(t, codex) != codexBefore || strings.Contains(readFixture(t, claude), "hook claude") {
		t.Fatal("agent selection not respected")
	}
	runHooksCLI(t, "y\n", "uninstall", "--agent", "codex")
	if strings.Contains(readFixture(t, codex), "notify") {
		t.Fatal("Codex notify not removed")
	}
}

func TestHooksCLICodexForeignNotify(t *testing.T) {
	home, codexHome := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("CODEX_HOME", codexHome)
	path := filepath.Join(codexHome, "config.toml")
	before := "notify = ['another-tool']\n"
	writeFixture(t, path, before)
	out := runHooksCLI(t, "yes\n", "install", "--agent", "codex")
	got := readFixture(t, path)
	if !strings.Contains(out, "Apply?") || !strings.HasPrefix(got, before) || !strings.Contains(got, "hook codex PermissionRequest") {
		t.Fatalf("install = %q; file %q", out, got)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Fatal("Codex-only install touched Claude")
	}
}
