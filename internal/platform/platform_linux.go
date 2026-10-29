//go:build linux

package platform

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/unix"
)

type linuxPlatform struct{ procFS }

var _ Platform = linuxPlatform{}
var _ SocketProber = linuxPlatform{}

// New returns the Linux host collector.
func New() Platform { return newLinux("") }

func newLinux(procRoot string) Platform {
	if procRoot == "" {
		procRoot = "/proc"
	}
	p := linuxPlatform{procFS{root: procRoot}}
	// Socket diagnostics describe the current kernel, never an injected tree.
	if procRoot == "/proc" {
		p.unixPeers = linuxUnixPeers
	}
	return p
}

func (p linuxPlatform) Procs(ctx context.Context) ([]model.Proc, error) {
	ctx = p.context(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.Proc, 0, len(procs))
	ttys := ttyDevices()
	for _, proc := range procs {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		ppid, err := proc.PpidWithContext(ctx)
		if err != nil {
			continue
		}
		start, err := proc.CreateTimeWithContext(ctx)
		if err != nil {
			continue
		}
		stat, _ := os.ReadFile(filepath.Join(p.root, strconv.FormatInt(int64(proc.Pid), 10), "stat"))
		states, _ := proc.StatusWithContext(ctx)
		if slices.Contains(states, process.Zombie) {
			out = append(out, model.Proc{PID: proc.Pid, PPID: ppid, StartTime: time.UnixMilli(start), Zombie: true})
			continue
		}
		rss, err := proc.MemoryInfoWithContext(ctx)
		if err != nil {
			continue
		}
		times, err := proc.TimesWithContext(ctx)
		if err != nil {
			continue
		}
		argv, err := proc.CmdlineSliceWithContext(ctx)
		if err != nil {
			continue
		}
		exe, err := proc.ExeWithContext(ctx)
		if err != nil {
			continue
		}
		status, _ := os.ReadFile(filepath.Join(p.root, strconv.FormatInt(int64(proc.Pid), 10), "status"))
		owner, ownerKnown := procOwnerUID(status)
		out = append(out, model.Proc{
			OwnerUID: owner, OwnerKnown: ownerKnown,
			Swap: ParseVmSwap(status), MemoryMeasure: "rss_swap",
			TTY: ttys[controllingTTYDevice(stat)],
			PID: proc.Pid, PPID: ppid, StartTime: time.UnixMilli(start),
			RSS: rss.RSS, CPUSeconds: times.User + times.System,
			Cmdline: strings.Join(argv, " "), Argv: argv, Exe: exe,
		})
	}
	return out, ctx.Err()
}

func (p linuxPlatform) Alive(pid int32, start time.Time) bool {
	if pid <= 0 || start.IsZero() {
		return false
	}
	ctx := p.context(context.Background())
	proc, err := process.NewProcessWithContext(ctx, pid)
	if err != nil {
		return false
	}
	ms, err := proc.CreateTimeWithContext(ctx)
	return err == nil && SameStart(time.UnixMilli(ms), start)
}

// linuxUnixPeers asks the kernel for UNIX_DIAG_PEER inode relationships, which
// /proc/net/unix omits. Unsupported/denied diagnostics leave peers unresolved.
func linuxUnixPeers(ctx context.Context) map[uint64]uint64 {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, unix.NETLINK_SOCK_DIAG)
	if err != nil {
		return nil
	}
	file := os.NewFile(uintptr(fd), "unix-sock-diag")
	defer file.Close()
	deadline, _ := ctx.Deadline()
	if err := file.SetReadDeadline(deadline); err != nil {
		return nil
	}
	// nlmsghdr + unix_diag_req, per linux/{netlink,unix_diag}.h.
	request := make([]byte, 40)
	order := binary.NativeEndian
	order.PutUint32(request[0:4], uint32(len(request)))
	order.PutUint16(request[4:6], 20) // SOCK_DIAG_BY_FAMILY
	order.PutUint16(request[6:8], unix.NLM_F_REQUEST|unix.NLM_F_DUMP)
	order.PutUint32(request[8:12], 1)
	request[16] = unix.AF_UNIX
	order.PutUint32(request[20:24], ^uint32(0)) // all states
	order.PutUint32(request[28:32], 4)          // UDIAG_SHOW_PEER
	order.PutUint32(request[32:36], ^uint32(0))
	order.PutUint32(request[36:40], ^uint32(0))
	if err := unix.Sendto(fd, request, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return nil
	}
	peers := make(map[uint64]uint64)
	buf := make([]byte, 64*1024)
	for ctx.Err() == nil {
		n, err := file.Read(buf)
		if err != nil || n == 0 || n == len(buf) {
			return nil
		}
		messages, err := syscall.ParseNetlinkMessage(buf[:n])
		if err != nil {
			return nil
		}
		for _, message := range messages {
			if message.Header.Seq != 1 || message.Header.Flags&unix.NLM_F_DUMP_INTR != 0 {
				return nil
			}
			switch message.Header.Type {
			case unix.NLMSG_DONE:
				return peers
			case unix.NLMSG_ERROR:
				return nil
			case 20:
				inode, peer := parseUnixDiagPeer(message.Data)
				if inode != 0 && peer != 0 {
					peers[inode] = peer
				}
			}
		}
	}
	return nil
}

// UserSubreaper returns the current user's systemd --user pid, if running.
func (p linuxPlatform) UserSubreaper() (int32, bool) {
	return UserSubreaper(p.root, os.Getuid())
}
