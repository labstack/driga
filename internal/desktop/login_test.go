//go:build darwin || windows

package desktop

import "testing"

// macOS login launches carry DRIGA_APP=1 and Windows ones run Driga.exe, so
// the CLI never asks the toolkit, which would load it before the app runs.
func TestOpenedAtLoginNeverAsks(t *testing.T) {
	if OpenedAtLogin() {
		t.Fatal("OpenedAtLogin() = true")
	}
}
