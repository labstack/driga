package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestGUIExecutable(t *testing.T) {
	for _, tt := range []struct{ exe, want string }{
		{`C:\Program Files\Driga\bin\driga.exe`, `C:\Program Files\Driga\Driga.exe`},
		{`C:\Program Files\Driga\BIN\driga.exe`, `C:\Program Files\Driga\Driga.exe`},
		{`C:\src\driga\driga.exe`, ""},
	} {
		if got := guiExecutable(tt.exe); got != tt.want {
			t.Errorf("guiExecutable(%q) = %q, want %q", tt.exe, got, tt.want)
		}
	}
}

func TestHandOffTo(t *testing.T) {
	defer func(prev func(*exec.Cmd) error) { startApp = prev }(startApp)
	var started *exec.Cmd
	failure := errors.New("start failed")
	startApp = func(cmd *exec.Cmd) error { started = cmd; return failure }

	if handed, err := handOffTo(""); handed || err != nil || started != nil {
		t.Fatalf("no app: %v %v", handed, err)
	}
	if handed, err := handOffTo(filepath.Join(t.TempDir(), "Driga.exe")); handed || err != nil || started != nil {
		t.Fatalf("missing app: %v %v", handed, err)
	}
	app := filepath.Join(t.TempDir(), "Driga.exe")
	if err := os.WriteFile(app, nil, 0600); err != nil {
		t.Fatal(err)
	}
	// A failed start is still a hand-off: the error is the command's.
	if handed, err := handOffTo(app); !handed || !errors.Is(err, failure) {
		t.Fatalf("failed start: %v %v", handed, err)
	}
	if started == nil || started.Path != app || started.SysProcAttr.CreationFlags&windows.DETACHED_PROCESS == 0 || started.SysProcAttr.CreationFlags&windows.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatalf("started %+v", started)
	}
}
