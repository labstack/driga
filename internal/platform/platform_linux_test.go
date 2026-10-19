//go:build linux

package platform

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/unix"
)

func TestLinuxProcsIncludesSelf(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	procs, err := newLinux("").Procs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range procs {
		if p.PID == int32(os.Getpid()) {
			if p.PPID != int32(os.Getppid()) || p.StartTime.IsZero() || p.RSS == 0 || p.Exe == "" || p.Cmdline == "" || p.CPUSeconds < 0 || p.CPUPercent != 0 || p.Cwd != "" {
				t.Fatalf("incomplete process: %+v", p)
			}
			return
		}
	}
	t.Fatal("self missing from process snapshot")
}

func TestLinuxAliveRejectsWrongStart(t *testing.T) {
	pid := int32(os.Getpid())
	p, err := process.NewProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	ms, err := p.CreateTime()
	if err != nil {
		t.Fatal(err)
	}
	start := time.UnixMilli(ms)
	fs := newLinux("")
	if !fs.Alive(pid, start) || !fs.Alive(pid, start.Add(30*time.Second)) {
		t.Fatal("self rejected")
	}
	if fs.Alive(pid, start.Add(-10*time.Second)) || fs.Alive(pid, start.Add(61*time.Second)) || fs.Alive(pid, time.Time{}) || fs.Alive(-1, start) {
		t.Fatal("wrong identity accepted")
	}
	if newLinux(t.TempDir()).Alive(pid, start) {
		t.Fatal("procRoot override ignored")
	}
}

func TestLinuxUnixPeersSocketpair(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range fds {
		defer unix.Close(fd)
	}
	var left, right unix.Stat_t
	if err := unix.Fstat(fds[0], &left); err != nil {
		t.Fatal(err)
	}
	if err := unix.Fstat(fds[1], &right); err != nil {
		t.Fatal(err)
	}
	peers := linuxUnixPeers(context.Background())
	if peers[left.Ino] != right.Ino || peers[right.Ino] != left.Ino {
		t.Fatalf("socketpair peer inodes missing: %d -> %d, %d -> %d", left.Ino, peers[left.Ino], right.Ino, peers[right.Ino])
	}
}
