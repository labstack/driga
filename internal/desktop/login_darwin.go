package desktop

// OpenedAtLogin reports whether the system started this process at login.
// On macOS the login item launches through Launch Services, whose
// LSEnvironment sets DRIGA_APP=1, and the toolkit only knows the answer once
// the app runs, so the question is never asked of it.
func OpenedAtLogin() bool { return false }
