package platform

import (
	"encoding/binary"
	"fmt"

	"github.com/labstack/driga/internal/model"
)

// parseWindowsSocketTable reads the fixed DWORD layouts from IP Helper, never
// native Go struct padding. Like Unix, any TCP connection/listener is serving
// evidence (even a loopback connection); every bound UDP endpoint is a listener.
// UDP owner tables have no peer information, so treating a binding as active
// is deliberately conservative.
func parseWindowsSocketTable(b []byte, family int, udp bool, pids []int32) (map[int32]model.PeerState, error) {
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
	result := make(map[int32]model.PeerState, len(pids))
	for _, pid := range pids {
		if pid > 0 {
			result[pid] = model.PeerNone
		}
	}
	for i := 0; i < int(n); i++ {
		r := b[4+i*row : 4+(i+1)*row]
		pid := int32(binary.LittleEndian.Uint32(r[pidOffset:]))
		if _, wanted := result[pid]; !wanted {
			continue
		}
		// A bound UDP endpoint can exchange datagrams with anyone, so it
		// counts as connected.
		seen := model.PeerConnected
		if !udp {
			// CLOSED, TIME_WAIT and DELETE_TCB hold no live connection; LISTEN
			// waits for one. All other (including unrecognized future) states
			// fail toward connected.
			switch binary.LittleEndian.Uint32(r[stateOffset:]) {
			case 1, 11, 12:
				seen = model.PeerNone
			case 2:
				seen = model.PeerListening
			}
		}
		result[pid] = max(result[pid], seen)
	}
	return result, nil
}
