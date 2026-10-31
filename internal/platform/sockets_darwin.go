package platform

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The proc_info syscall that libproc's proc_listallpids, proc_pidinfo and
// proc_pidfdinfo wrap, and the layout of struct socket_fdinfo from the macOS
// SDK's sys/proc_info.h. Offsets match for arm64 and x86_64.
// https://github.com/apple-oss-distributions/xnu/blob/main/bsd/sys/proc_info.h
const (
	procInfoPidInfo   = 2 // PROC_INFO_CALL_PIDINFO
	procInfoPidFdInfo = 3 // PROC_INFO_CALL_PIDFDINFO

	procPidListFds      = 1 // PROC_PIDLISTFDS
	procPidFdSocketInfo = 3 // PROC_PIDFDSOCKETINFO
	proxFdTypeSocket    = 2 // PROX_FDTYPE_SOCKET

	sizeofProcFdInfo   = 8   // struct proc_fdinfo
	sizeofSocketFdInfo = 792 // struct socket_fdinfo
	maxProcFds         = 1 << 20

	offSocketPcb  = 168                // psi.soi_pcb: lsof's d for a Unix socket
	offSocketKind = 256                // psi.soi_kind
	offSocketProt = 264                // psi.soi_proto union
	offUnConnPcb  = offSocketProt + 8  // unsi_conn_pcb: lsof's ->0x peer
	offUnAddr     = offSocketProt + 16 // unsi_addr: sockaddr_un in a 255-byte union
	offInFport    = offSocketProt + 0  // insi_fport, network order in an int
	offInLport    = offSocketProt + 4  // insi_lport
	offInVflag    = offSocketProt + 24 // insi_vflag: INI_IPV4 = 1
	offInFaddr    = offSocketProt + 32 // insi_faddr
	offInLaddr    = offSocketProt + 48 // insi_laddr
	offTCPState   = offSocketProt + 80 // tcpsi_state
	socketKindIn  = 1                  // SOCKINFO_IN
	socketKindTCP = 2                  // SOCKINFO_TCP
	socketKindUn  = 3                  // SOCKINFO_UN
)

// errSocketLayout means the kernel's socket_fdinfo no longer matches the
// layout above; callers fall back to lsof.
var errSocketLayout = errors.New("socket_fdinfo layout changed")

var tcpStateNames = [...]string{"CLOSED", "LISTEN", "SYN_SENT", "SYN_RECEIVED", "ESTABLISHED", "CLOSE_WAIT", "FIN_WAIT_1", "CLOSING", "LAST_ACK", "FIN_WAIT_2", "TIME_WAIT"}

// procInfo calls proc_info with buf as its buffer.
func procInfo(call, pid, flavor int, arg uint64, buf []byte) (int, error) {
	var p unsafe.Pointer
	if len(buf) > 0 {
		p = unsafe.Pointer(&buf[0])
	}
	return procInfoAt(call, pid, flavor, arg, p, len(buf))
}

// procInfoAt is the raw proc_info syscall.
func procInfoAt(call, pid, flavor int, arg uint64, buf unsafe.Pointer, size int) (int, error) {
	n, _, errno := syscall.Syscall6(unix.SYS_PROC_INFO, uintptr(call), uintptr(pid), uintptr(flavor), uintptr(arg), uintptr(buf), uintptr(size))
	if errno != 0 {
		return 0, errno
	}
	return int(n), nil
}

// socketReader reads processes' sockets, reusing its buffers across them.
type socketReader struct{ fds, info []byte }

// sockets appends pid's sockets as lsof-equivalent records to out. ESRCH means
// the process exited and EPERM that it cannot be read; both are returned for
// the caller to classify.
func (r *socketReader) sockets(pid int32, out []lsofSocket) ([]lsofSocket, error) {
	// With no buffer, PROC_PIDLISTFDS returns the size it needs; leave room
	// for descriptors opened before the second call.
	need, err := procInfo(procInfoPidInfo, int(pid), procPidListFds, 0, nil)
	if err != nil {
		return out, err
	}
	var fds []byte
	for size := need + 32*sizeofProcFdInfo; ; size *= 2 {
		if size > maxProcFds*sizeofProcFdInfo {
			return out, fmt.Errorf("pid %d: too many open files", pid)
		}
		if cap(r.fds) < size {
			r.fds = make([]byte, size)
		}
		n, err := procInfo(procInfoPidInfo, int(pid), procPidListFds, 0, r.fds[:size])
		if err != nil {
			return out, err
		}
		// A full buffer may have been truncated.
		if n < size {
			fds = r.fds[:n]
			break
		}
	}
	if r.info == nil {
		r.info = make([]byte, sizeofSocketFdInfo)
	}
	for i := 0; i+sizeofProcFdInfo <= len(fds); i += sizeofProcFdInfo {
		if binary.LittleEndian.Uint32(fds[i+4:]) != proxFdTypeSocket {
			continue
		}
		fd := binary.LittleEndian.Uint32(fds[i:])
		n, err := procInfo(procInfoPidFdInfo, int(pid), procPidFdSocketInfo, uint64(fd), r.info)
		switch {
		case errors.Is(err, syscall.EBADF), errors.Is(err, syscall.ENOTSOCK), errors.Is(err, syscall.EOPNOTSUPP):
			// Closed, or reused for something else, between listing and reading.
			continue
		case errors.Is(err, syscall.ENOMEM):
			// The kernel's struct outgrew the buffer.
			return out, fmt.Errorf("pid %d fd %d: %w", pid, fd, errSocketLayout)
		case err != nil:
			return out, err
		case n != sizeofSocketFdInfo:
			return out, fmt.Errorf("pid %d fd %d: %d bytes: %w", pid, fd, n, errSocketLayout)
		}
		if s, ok := decodeSocketFdInfo(pid, r.info); ok {
			out = append(out, s)
		}
	}
	return out, nil
}

// decodeSocketFdInfo converts one struct socket_fdinfo into the record lsof
// would print for it: a Unix socket's own and peer kernel addresses and bound
// path, or an Internet socket's endpoints and TCP state. A connection a server
// accepted carries its client's address here, which lsof hides.
func decodeSocketFdInfo(pid int32, b []byte) (lsofSocket, bool) {
	s := lsofSocket{pid: pid}
	u32 := func(o int) uint32 { return binary.LittleEndian.Uint32(b[o:]) }
	u64 := func(o int) uint64 { return binary.LittleEndian.Uint64(b[o:]) }
	switch u32(offSocketKind) {
	case socketKindUn:
		s.kind, s.device = "unix", fmt.Sprintf("%#x", u64(offSocketPcb))
		s.name = sockaddrUnPath(b[offUnAddr : offUnAddr+255])
		if peer := u64(offUnConnPcb); peer != 0 {
			s.name += fmt.Sprintf("->%#x", peer)
		}
	case socketKindIn, socketKindTCP:
		v4 := b[offInVflag]&1 != 0
		s.kind = "IPv6"
		if v4 {
			s.kind = "IPv4"
		}
		port := func(o int) uint16 { p := uint16(u32(o)); return p>>8 | p<<8 }
		s.name = netip.AddrPortFrom(inAddr(b[offInLaddr:], v4), port(offInLport)).String()
		if remote := port(offInFport); remote != 0 {
			s.name += "->" + netip.AddrPortFrom(inAddr(b[offInFaddr:], v4), remote).String()
		}
		if u32(offSocketKind) == socketKindTCP {
			if state := u32(offTCPState); int(state) < len(tcpStateNames) {
				s.state = tcpStateNames[state]
			}
		}
	default:
		return s, false
	}
	return s, true
}

// sockaddrUnPath reads sun_path from struct sockaddr_un (sun_len, sun_family,
// sun_path).
func sockaddrUnPath(b []byte) string {
	n := int(b[0])
	if n <= 2 {
		return ""
	}
	path := b[2:min(n, len(b))]
	for i, c := range path {
		if c == 0 {
			return string(path[:i])
		}
	}
	return string(path)
}

// inAddr reads struct in4in6_addr (IPv4 in its last four bytes) or in6_addr.
func inAddr(b []byte, v4 bool) netip.Addr {
	if v4 {
		return netip.AddrFrom4([4]byte(b[12:16]))
	}
	return netip.AddrFrom16([16]byte(b[:16])).Unmap()
}

// errScanDeadline means a socket scan ran out of its own time, not the
// caller's.
var errScanDeadline = errors.New("socket scan deadline exceeded")

// scanDeadline bounds a scan: five seconds per 200 tree pids, plus ten for
// the scan of every process.
func scanDeadline(pids []int32, all bool) time.Duration {
	d := time.Duration(max(1, (len(pids)+lsofBatch-1)/lsofBatch)) * lsofBatchDeadline
	if all {
		d += lsofAllDeadline
	}
	return d
}

// socketScan is the result of scanSockets. unreadable lists tree pids whose
// sockets could not be read; complete is false when another process of this
// user could not be read, so a client may be missing.
type socketScan struct {
	sockets    []lsofSocket
	unreadable []int32
	complete   bool
}

// scanSockets reads every socket of the tree pids and, with all set, the Unix
// sockets of every other process this user owns, each process once. A process
// that exited reports nothing.
func scanSockets(ctx context.Context, pids []int32, all bool) (socketScan, error) {
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, scanDeadline(pids, all))
	defer cancel()
	tree := make(map[int32]bool, len(pids))
	targets := make([]int32, 0, len(pids))
	for _, pid := range pids {
		if pid > 0 && !tree[pid] {
			tree[pid] = true
			targets = append(targets, pid)
		}
	}
	scan := socketScan{complete: true}
	if all {
		// One snapshot names every process and its owner, so other users'
		// processes, unreadable without root, are never asked.
		kprocs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
		if err != nil {
			scan.complete = false
		}
		uid := uint32(os.Getuid())
		for _, k := range kprocs {
			if pid := k.Proc.P_pid; pid > 0 && !tree[pid] && k.Eproc.Ucred.Uid == uid {
				targets = append(targets, pid)
			}
		}
	}
	var r socketReader
	for _, pid := range targets {
		if err := ctx.Err(); err != nil {
			if parent.Err() != nil {
				return socketScan{}, parent.Err()
			}
			return socketScan{}, errScanDeadline
		}
		start := len(scan.sockets)
		var err error
		scan.sockets, err = r.sockets(pid, scan.sockets)
		switch {
		case err == nil:
		case errors.Is(err, syscall.ESRCH):
		case errors.Is(err, errSocketLayout):
			return socketScan{}, err
		case tree[pid]:
			scan.unreadable = append(scan.unreadable, pid)
		default:
			scan.complete = false
		}
		if err != nil {
			scan.sockets = scan.sockets[:start]
			continue
		}
		if !tree[pid] {
			// Other processes matter only as Unix peers.
			kept := scan.sockets[:start]
			for _, s := range scan.sockets[start:] {
				if s.kind == "unix" {
					kept = append(kept, s)
				}
			}
			scan.sockets = kept
		}
	}
	return scan, nil
}
