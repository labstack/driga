package tui

import (
	"reflect"
	"runtime"
	"testing"

	"github.com/labstack/driga/internal/model"
)

func fixtureCwd() string {
	if runtime.GOOS == "windows" {
		return `C:\fixture\repo with spaces`
	}
	return "/tmp/repo with spaces"
}

// Windows opens the recorded directory; Unix fixtures exercise tmux/resume.
func fixtureLocalJump(t *testing.T, j model.InboxJump) *model.InboxJump {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Setenv("PATH", t.TempDir()) // Deterministically select the cmd.exe fallback.
		j.Kind = "terminal"
		j.Cwd = fixtureCwd()
	}
	return &j
}

func requireFixtureTerminal(t *testing.T, args []string) {
	t.Helper()
	want := []string{"cmd.exe", "/c", "start", "", "/D", fixtureCwd(), "cmd.exe"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("terminal command = %q; want %q", args, want)
	}
}
