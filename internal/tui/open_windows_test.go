package tui

import (
	"reflect"
	"testing"
)

func TestWindowsDefaultOpenTerminal(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	cwd := `C:\work & 界`
	if got := openCommand("", "", cwd); !reflect.DeepEqual(got, []string{"cmd.exe", "/c", "start", "", "/D", cwd, "cmd.exe"}) {
		t.Fatal(got)
	}
}
