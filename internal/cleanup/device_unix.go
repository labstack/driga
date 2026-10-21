//go:build !windows

package cleanup

import (
	"os"
	"syscall"
)

func sameDevice(a, b *syscall.Stat_t) bool { return a.Dev == b.Dev }
func deviceID(info os.FileInfo) (uint64, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(stat.Dev), true
}

func devicePath(_ string, info os.FileInfo) (uint64, bool) { return deviceID(info) }
