//go:build darwin || linux

package platform

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// ttyDevices maps kernel device IDs using character devices observed once per
// process scan. Missing/inaccessible devices simply produce no jump proof.
func ttyDevices() map[uint64]string {
	result := map[uint64]string{}
	for _, dir := range []string{"/dev", "/dev/pts"} {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() || dir == "/dev" && !strings.HasPrefix(e.Name(), "tty") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			var st unix.Stat_t
			if unix.Stat(path, &st) == nil && st.Mode&unix.S_IFMT == unix.S_IFCHR {
				result[uint64(st.Rdev)] = path
			}
		}
	}
	return result
}

func controllingTTYDevice(stat []byte) uint64 {
	end := strings.LastIndexByte(string(stat), ')')
	if end < 0 {
		return 0
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 5 {
		return 0
	}
	n, err := strconv.ParseUint(fields[4], 10, 32)
	if err != nil {
		return 0
	}
	return n
}
