package desktop

// OpenedAtLogin reports whether the system started this process at login.
// On Windows the login entry is registered by Driga.exe, which always runs
// the app, so the CLI never starts at login.
func OpenedAtLogin() bool { return false }
