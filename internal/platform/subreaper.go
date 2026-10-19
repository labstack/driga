package platform

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// UserSubreaper finds a systemd --user process belonging to uid in procRoot.
// Missing or unreadable proc entries are skipped. An empty root means /proc.
func UserSubreaper(procRoot string, uid int) (int32, bool) {
	if uid < 0 {
		return 0, false
	}
	if procRoot == "" {
		procRoot = "/proc"
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return 0, false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.ParseInt(entry.Name(), 10, 32)
		if err != nil || pid <= 1 {
			continue
		}
		dir := filepath.Join(procRoot, entry.Name())
		comm, err := os.ReadFile(filepath.Join(dir, "comm"))
		if err != nil || strings.TrimSpace(string(comm)) != "systemd" {
			continue
		}
		status, err := os.ReadFile(filepath.Join(dir, "status"))
		if err != nil {
			continue
		}
		owned := false
		for _, line := range strings.Split(string(status), "\n") {
			f := strings.Fields(line)
			if len(f) != 5 || f[0] != "Uid:" {
				continue
			}
			// Require real and effective uid to agree with the requested user.
			real, e1 := strconv.Atoi(f[1])
			effective, e2 := strconv.Atoi(f[2])
			owned = e1 == nil && e2 == nil && real == uid && effective == uid
			break
		}
		if !owned {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join(dir, "cmdline"))
		if err != nil {
			continue
		}
		args := bytes.Split(cmdline, []byte{0})
		if len(args) < 2 || filepath.Base(string(args[0])) != "systemd" {
			continue
		}
		for _, arg := range args[1:] {
			if string(arg) == "--user" {
				return int32(pid), true
			}
		}
	}
	return 0, false
}
