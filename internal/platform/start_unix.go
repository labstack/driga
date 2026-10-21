//go:build darwin || linux

package platform

import (
	"github.com/shirou/gopsutil/v4/process"
	"time"
)

// ProcessStart queries only the requested identity; it never scans the process table.
func ProcessStart(pid int32) (time.Time, error) {
	p, err := process.NewProcess(pid)
	if err != nil {
		return time.Time{}, err
	}
	ms, err := p.CreateTime()
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(ms), nil
}
