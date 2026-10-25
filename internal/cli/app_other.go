//go:build !darwin && !windows

package cli

// handOffApp never hands off: the one binary is the app.
func handOffApp(bool) (bool, error) { return false, nil }
