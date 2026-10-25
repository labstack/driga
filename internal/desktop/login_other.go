//go:build !darwin && !windows

package desktop

import "github.com/egoist/mygo"

// OpenedAtLogin reports whether the system started this process at login,
// through the autostart entry's login argument. Call it on the main goroutine,
// before Run.
func OpenedAtLogin() bool { return mygo.App.WasOpenedAtLogin() }
