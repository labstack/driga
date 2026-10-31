package platform

import (
	"context"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/labstack/driga/internal/model"
)

var ipHelper = windows.NewLazySystemDLL("iphlpapi.dll")
var extendedTCP = ipHelper.NewProc("GetExtendedTcpTable")
var extendedUDP = ipHelper.NewProc("GetExtendedUdpTable")

var _ SocketProber = winPlatform{}

func (p winPlatform) ExternalPeers(ctx context.Context, pids []int32) (map[int32]model.PeerState, error) {
	result := make(map[int32]model.PeerState, len(pids))
	for _, pid := range pids {
		if pid > 0 {
			result[pid] = model.PeerNone
		}
	}
	for _, family := range []int{2, 23} {
		for _, udp := range []bool{false, true} {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			b, err := windowsSocketTable(family, udp)
			if err != nil {
				return nil, err
			}
			peers, err := parseWindowsSocketTable(b, family, udp, pids)
			if err != nil {
				return nil, err
			}
			for pid, state := range peers {
				result[pid] = max(result[pid], state)
			}
		}
	}
	return result, ctx.Err()
}

func windowsSocketTable(family int, udp bool) ([]byte, error) {
	proc, class := extendedTCP, uintptr(5) // TCP_TABLE_OWNER_PID_ALL
	if udp {
		proc, class = extendedUDP, 1
	} // UDP_TABLE_OWNER_PID
	var size uint32
	for attempt := 0; attempt < 5; attempt++ {
		if size > 64<<20 {
			return nil, fmt.Errorf("socket table exceeds safety limit")
		}
		b := make([]byte, size)
		var ptr uintptr
		if len(b) > 0 {
			ptr = uintptr(unsafe.Pointer(&b[0]))
		}
		code, _, _ := proc.Call(ptr, uintptr(unsafe.Pointer(&size)), 0, uintptr(family), class, 0)
		if code == 0 {
			if size > uint32(len(b)) {
				return nil, fmt.Errorf("socket table grew unexpectedly")
			}
			return b[:size], nil
		}
		if code != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) {
			return nil, fmt.Errorf("%s: %w", proc.Name, windows.Errno(code))
		}
	}
	return nil, fmt.Errorf("socket table changed repeatedly")
}

// No reliable unprivileged directory-wide open-file API is available. Keeping
// FileProber unimplemented makes cleanup's capability check return unknown.
