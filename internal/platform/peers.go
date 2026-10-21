package platform

import (
	"strconv"
	"strings"
)

// ParseLsofPeers parses lsof -FpcnDtT socket records for the given tree.
// An unresolved Unix peer is external; listening sockets also count as serving.
func ParseLsofPeers(out []byte, pids []int32) map[int32]bool {
	type socket struct {
		pid                       int32
		device, kind, name, state string
	}
	wanted := make(map[int32]bool, len(pids))
	result := make(map[int32]bool, len(pids))
	for _, pid := range pids {
		if pid > 0 {
			wanted[pid], result[pid] = true, false
		}
	}
	var sockets []socket
	var pid int32
	var current socket
	flush := func() {
		if current.pid > 0 {
			sockets = append(sockets, current)
		}
		current = socket{pid: pid}
	}
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			flush()
			pid = 0
			if n, err := strconv.ParseInt(line[1:], 10, 32); err == nil && n > 0 {
				pid = int32(n)
			}
			current = socket{pid: pid}
		case 'f':
			flush()
		case 'D':
			current.device = line[1:]
		case 't':
			current.kind = line[1:]
		case 'n':
			current.name = line[1:]
		case 'T':
			if state, ok := strings.CutPrefix(line[1:], "ST="); ok {
				current.state = state
			}
		}
	}
	flush()
	// Collect endpoint ownership before resolving peers: the peer's process
	// record may occur after the connecting process in lsof's output.
	owners := make(map[string][]int32)
	for _, s := range sockets {
		if s.kind != "unix" && !strings.Contains(s.name, "->0x") && !strings.HasPrefix(s.name, "/") {
			continue
		}
		local, _, _ := strings.Cut(s.name, "->")
		for _, endpoint := range []string{s.device, local} {
			if endpoint = socketEndpoint(endpoint); endpoint != "" {
				owners[endpoint] = append(owners[endpoint], s.pid)
			}
		}
	}
	for _, s := range sockets {
		if !wanted[s.pid] {
			continue
		}
		if s.state == "ESTABLISHED" || s.state == "LISTEN" || strings.Contains(s.name, "(ESTABLISHED)") || strings.Contains(s.name, "(LISTEN)") {
			result[s.pid] = true
			continue
		}
		_, peer, connected := strings.Cut(s.name, "->")
		if connected {
			peerOwners := owners[socketEndpoint(peer)]
			external := len(peerOwners) == 0
			for _, owner := range peerOwners {
				if !wanted[owner] {
					external = true
				}
			}
			if external {
				result[s.pid] = true
			}
		} else if (s.kind == "unix" || strings.HasPrefix(s.name, "/")) && socketEndpoint(s.name) != "" {
			// A bound Unix socket with no connected endpoint is serving too.
			result[s.pid] = true
		}
	}
	return result
}

func socketEndpoint(name string) string {
	name, _, _ = strings.Cut(strings.TrimSpace(name), " type=")
	name, _, _ = strings.Cut(name, " (")
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "0x") {
		return "0x" + strings.TrimLeft(strings.ToLower(strings.TrimPrefix(name, "0x")), "0")
	}
	return name
}
