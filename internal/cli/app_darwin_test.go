package cli

import (
	"slices"
	"testing"
)

func TestAppBundle(t *testing.T) {
	for _, tt := range []struct{ exe, want string }{
		{"/Applications/Driga.app/Contents/MacOS/driga", "/Applications/Driga.app"},
		{"/Users/someone/Applications/Driga.app/Contents/MacOS/driga", "/Users/someone/Applications/Driga.app"},
		{"/opt/homebrew/bin/driga", ""},
		{"/tmp/build/driga", ""},
		{"/Applications/Driga.app/Contents/Resources/driga", ""},
	} {
		if got := appBundle(tt.exe); got != tt.want {
			t.Errorf("appBundle(%q) = %q, want %q", tt.exe, got, tt.want)
		}
	}
}

func TestAppHandOff(t *testing.T) {
	const bundled = "/Applications/Driga.app/Contents/MacOS/driga"
	for _, tt := range []struct {
		system bool
		exe    string
		want   bool
	}{
		{false, bundled, true},
		{true, bundled, false},
		{false, "/tmp/build/driga", false},
	} {
		if got := handsOff(tt.system, tt.exe); got != tt.want {
			t.Errorf("handsOff(%v, %q) = %v, want %v", tt.system, tt.exe, got, tt.want)
		}
	}
}

// The hand-off opens the bundle the command line ran from, not whichever
// bundle Launch Services knows by the identifier, with the command line's
// DRIGA_* variables.
func TestAppHandOffCommand(t *testing.T) {
	cmd := handOffCommand("/Users/someone/Applications/Driga.app", []string{"PATH=/bin", "DRIGA_DATA_DIR=/data dir", "DRIGA_APP=1", "DRIGA_CONFIG="})
	if want := []string{"open", "--env", "DRIGA_DATA_DIR=/data dir", "--env", "DRIGA_CONFIG=", "-a", "/Users/someone/Applications/Driga.app"}; !slices.Equal(cmd.Args, want) {
		t.Fatalf("hand-off runs %q, want %q", cmd.Args, want)
	}
}
