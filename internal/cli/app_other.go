//go:build !darwin && !windows

package cli

// handOffApp never hands off: the one binary is the app.
func handOffApp(bool, ...string) (bool, error) { return false, nil }
