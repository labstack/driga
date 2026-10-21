package cleanup

import (
	"path/filepath"
	"time"
)

func trashDirs(home string) (string, string, error)             { return filepath.Join(home, ".Trash"), "", nil }
func trashInfo(_, _, _ string, _ time.Time) (func(bool), error) { return func(bool) {}, nil }
