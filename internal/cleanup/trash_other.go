//go:build !darwin && !linux && !windows

package cleanup

import (
	"fmt"
	"time"
)

func trashDirs(string) (string, string, error) { return "", "", fmt.Errorf("OS trash unavailable") }
func trashInfo(_, _, _ string, _ time.Time) (func(bool), error) {
	return nil, fmt.Errorf("OS trash unavailable")
}
func renameScratch(Candidate, string) error { return fmt.Errorf("OS trash unavailable") }
