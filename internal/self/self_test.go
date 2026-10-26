package self

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCLI(t *testing.T) {
	for in, want := range map[string]string{
		"/Applications/Driga.app/Contents/MacOS/drigad": "/Applications/Driga.app/Contents/MacOS/driga",
		"/home/me/.local/libexec/drigad-dev":            "/home/me/.local/libexec/driga-dev",
		"/opt/drigad.exe":                               "/opt/driga.exe",
		"/opt/driga":                                    "/opt/driga",
		"/opt/driga-dev":                                "/opt/driga-dev",
		"/opt/drigadx":                                  "/opt/drigadx",
	} {
		if got := CLI(in); got != want {
			t.Errorf("CLI(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExecutableResolvesCLISibling(t *testing.T) {
	root := t.TempDir()
	daemon, cli := filepath.Join(root, "drigad-dev"), filepath.Join(root, "driga-dev")
	if got, err := executable(daemon); got != "" || !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing sibling: %q, %v", got, err)
	}
	if err := os.Mkdir(cli, 0755); err != nil {
		t.Fatal(err)
	}
	if got, err := executable(daemon); got != "" || err == nil || !strings.Contains(err.Error(), "not an executable file") {
		t.Errorf("directory sibling: %q, %v", got, err)
	}
	if err := os.Remove(cli); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cli, []byte("fake CLI"), 0755); err != nil {
		t.Fatal(err)
	}
	if got, err := executable(daemon); got != cli || err != nil {
		t.Fatalf("installed sibling: %q, %v", got, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(cli, 0644); err != nil {
			t.Fatal(err)
		}
		if got, err := executable(daemon); got != "" || err == nil {
			t.Errorf("non-executable sibling: %q, %v", got, err)
		}
	}
}

func TestExecutableKeepsCurrentCLI(t *testing.T) {
	current, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := executable(current); got != current || err != nil {
		t.Fatalf("current CLI: %q, %v", got, err)
	}
}
