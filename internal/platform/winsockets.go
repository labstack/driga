package platform

import (
	"encoding/binary"
	"fmt"
)

// parseWindowsSocketTable reads the fixed DWORD layouts from IP Helper, never
// native Go struct padding. Like Unix, any TCP connection/listener is serving
// evidence (even a loopback connection); every bound UDP endpoint is a listener.
// UDP owner tables have no peer information, so treating a binding as active
// is deliberately conservative.
func parseWindowsSocketTable(b []byte, family int, udp bool, pids []int32) (map[int32]bool, error) {
	row, pidOffset, stateOffset := 24, 20, 0
	switch {
	case family == 2 && udp:
		row, pidOffset = 12, 8
	case family == 23 && udp:
		row, pidOffset = 28, 24
	case family == 23:
		row, pidOffset, stateOffset = 56, 52, 48
	case family != 2:
		return nil, fmt.Errorf("unsupported socket address family %d", family)
	}
	if len(b) < 4 {
		return nil, fmt.Errorf("truncated socket table header")
	}
	n := uint64(binary.LittleEndian.Uint32(b))
	if n > uint64((len(b)-4)/row) {
		return nil, fmt.Errorf("truncated socket table rows")
	}
	result := make(map[int32]bool, len(pids))
	for _, pid := range pids {
		if pid > 0 {
			result[pid] = false
		}
	}
	for i := 0; i < int(n); i++ {
		r := b[4+i*row : 4+(i+1)*row]
		pid := int32(binary.LittleEndian.Uint32(r[pidOffset:]))
		if _, wanted := result[pid]; !wanted {
			continue
		}
		active := udp
		if !udp {
			state := binary.LittleEndian.Uint32(r[stateOffset:])
			// CLOSED, TIME_WAIT and DELETE_TCB hold no live connection. All
			// other (including unrecognized future) states fail toward active.
			active = state != 1 && state != 11 && state != 12
		}
		result[pid] = result[pid] || active
	}
	return result, nil
}
