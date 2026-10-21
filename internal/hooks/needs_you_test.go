package hooks_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/hooks"
)

func TestCodexPermissionReportOnlyByteRoundTrip(t *testing.T) {
	for _, before := range []string{"# bytes\nmodel = 'x' # keep\n[custom]\nvalue = 9007199254740993\n", "model='x'", ""} {
		p := filepath.Join(t.TempDir(), "config.toml")
		writeFixture(t, p, before)
		d, err := hooks.CodexInstall(p, "/opt/driga", false, testNow)
		if err != nil || d.FullRewrite || !strings.Contains(d.After, "[[hooks.PermissionRequest]]") || !strings.Contains(d.After, "timeout = 3") {
			t.Fatalf("unsafe/missing permission hook: %+v %v", d, err)
		}
		if !strings.Contains(d.After, before) {
			t.Fatal("unrelated bytes rewritten")
		}
		if _, err = hooks.CodexUninstall(p, false); err != nil {
			t.Fatal(err)
		}
		if got := readFixture(t, p); got != before {
			t.Fatalf("round-trip: %q != %q", got, before)
		}
	}
}

func TestCodexInboxPositionalInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	out := runHooksCLI(t, "", "install", "codex", "--dry-run")
	if !strings.Contains(out, "PermissionRequest") || strings.Contains(out, "hook claude") {
		t.Fatal(out)
	}
}
