package platform

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The proc_info syscall that libproc's proc_listallpids, proc_pidinfo and
// proc_pidfdinfo wrap, and the layout of struct socket_fdinfo from the macOS
// SDK's sys/proc_info.h. Offsets match for arm64 and x86_64.
// https://github.com/apple-oss-distributions/xnu/blob/main/bsd/sys/proc_info.h
const (
	procInfoListPids  = 1 // PROC_INFO_CALL_LISTPIDS
	procInfoPidInfo   = 2 // PROC_INFO_CALL_PIDINFO
	procInfoPidFdInfo = 3 // PROC_INFO_CALL_PIDFDINFO

	procAllPids         = 1 // PROC_ALL_PIDS
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

func procInfo(call, pid, flavor int, arg uint64, buf []byte) (int, error) {
	var p unsafe.Pointer
	if len(buf) > 0 {
		p = unsafe.Pointer(&buf[0])
	}
	n, _, errno := syscall.Syscall6(unix.SYS_PROC_INFO, uintptr(call), uintptr(pid), uintptr(flavor), uintptr(arg), uintptr(p), uintptr(len(buf)))
	if errno != 0 {
		return 0, errno
	}
	return int(n), nil
}

// procAllPIDs is proc_listallpids.
func procAllPIDs() ([]int32, error) {
	n, err := procInfo(procInfoListPids, procAllPids, 0, 0, nil)
	if err != nil {
		return nil, err
	}
	// Leave room for processes started between the two calls.
	buf := make([]byte, n+256*4)
	n, err = procInfo(procInfoListPids, procAllPids, 0, 0, buf)
	if err != nil {
		return nil, err
	}
	var pids []int32
	for i := 0; i+4 <= n; i += 4 {
		if pid := int32(binary.LittleEndian.Uint32(buf[i:])); pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

// procSockets returns pid's sockets as lsof-equivalent records. ESRCH means
// the process exited and EPERM that it belongs to another user; both are
// returned for the caller to classify.
func procSockets(pid int32) ([]lsofSocket, error) {
	var fds []byte
	for size := 64 * sizeofProcFdInfo; ; size *= 2 {
		if size > maxProcFds*sizeofProcFdInfo {
			return nil, fmt.Errorf("pid %d: too many open files", pid)
		}
		fds = make([]byte, size)
		n, err := procInfo(procInfoPidInfo, int(pid), procPidListFds, 0, fds)
		if err != nil {
			return nil, err
		}
		// A full buffer may have been truncated.
		if n < size {
			fds = fds[:n]
			break
		}
	}
	var sockets []lsofSocket
	info := make([]byte, sizeofSocketFdInfo)
	for i := 0; i+sizeofProcFdInfo <= len(fds); i += sizeofProcFdInfo {
		if binary.LittleEndian.Uint32(fds[i+4:]) != proxFdTypeSocket {
			continue
		}
		fd := binary.LittleEndian.Uint32(fds[i:])
		n, err := procInfo(procInfoPidFdInfo, int(pid), procPidFdSocketInfo, uint64(fd), info)
		if errors.Is(err, syscall.EBADF) {
			// Closed between listing and reading.
			continue
		}
		if err != nil {
			return nil, err
		}
		if n != sizeofSocketFdInfo {
			return nil, fmt.Errorf("pid %d fd %d: %d bytes: %w", pid, fd, n, errSocketLayout)
		}
		if s, ok := decodeSocketFdInfo(pid, info); ok {
			sockets = append(sockets, s)
		}
	}
	return sockets, nil
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

// treeSockets reads the sockets of pids. A pid that exited reports nothing;
// any other failure is returned.
func treeSockets(ctx context.Context, pids []int32) ([]lsofSocket, error) {
	var sockets []lsofSocket
	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if pid <= 0 {
			continue
		}
		s, err := procSockets(pid)
		if errors.Is(err, syscall.ESRCH) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("socket check pid %d: %w", pid, err)
		}
		sockets = append(sockets, s...)
	}
	return sockets, nil
}

// allUnixSockets reads every visible process's Unix sockets. complete is
// false when a process of this user could not be read, so a client may be
// missing; another user's processes are expected to be unreadable.
func allUnixSockets(ctx context.Context) (sockets []lsofSocket, complete bool, err error) {
	pids, err := procAllPIDs()
	if err != nil {
		return nil, false, nil
	}
	uid := uint32(os.Getuid())
	complete = true
	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		s, err := procSockets(pid)
		switch {
		case err == nil:
			for _, socket := range s {
				if socket.kind == "unix" {
					sockets = append(sockets, socket)
				}
			}
		case errors.Is(err, syscall.ESRCH):
		case errors.Is(err, syscall.EPERM) && !sameUser(pid, uid):
		case errors.Is(err, errSocketLayout):
			return nil, false, err
		default:
			complete = false
		}
	}
	return sockets, complete, nil
}

// sameUser reports whether pid runs as uid; an unknown owner counts as the
// same user, so it cannot hide a client.
func sameUser(pid int32, uid uint32) bool {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", int(pid))
	if err != nil {
		return !errors.Is(err, syscall.ESRCH)
	}
	return info.Eproc.Ucred.Uid == uid
}
