//go:build !darwin && !linux && !windows

package platform

import "time"

func ProcessStart(int32) (time.Time, error) { return time.Time{}, ErrUnsupported }
