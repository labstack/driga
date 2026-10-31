package platform

import (
	"strconv"
	"strings"

	"github.com/labstack/driga/internal/model"
)

// ParseLsofPeers parses lsof -FpcdnDtT socket records for the given tree.
// An unresolved Unix peer is external; a peer lsof shows as (none) has closed.
// Listening and bound Unix sockets report PeerListening.
func ParseLsofPeers(out []byte, pids []int32) map[int32]model.PeerState {
	return ParseLsofPeersAll(out, pids, nil)
}

// ParseLsofPeersAll is ParseLsofPeers with all, the same records for every
// process lsof can inspect. An outside socket whose peer is a tree socket makes
// that tree process connected: macOS lists a connection a server accepted only
// by its bound path, so only the client's side shows it. An unresolved peer no
// listed process owns belongs to another user, such as a system service, and
// reports PeerSystem. Without all, every unresolved peer stays connected.
func ParseLsofPeersAll(out []byte, pids []int32, all []byte) map[int32]model.PeerState {
	wanted := make(map[int32]bool, len(pids))
	result := make(map[int32]model.PeerState, len(pids))
	for _, pid := range pids {
		if pid > 0 {
			wanted[pid], result[pid] = true, model.PeerNone
		}
	}
	sockets := parseLsofSockets(out)
	// Collect endpoint ownership before resolving peers: the peer's process
	// record may occur after the connecting process in lsof's output.
	owners := make(map[string][]int32)
	for _, s := range sockets {
		for _, endpoint := range s.endpoints() {
			owners[endpoint] = append(owners[endpoint], s.pid)
		}
	}
	var visible map[string]bool
	if all != nil {
		visible = make(map[string]bool)
		for _, s := range parseLsofSockets(all) {
			for _, endpoint := range s.endpoints() {
				visible[endpoint] = true
			}
			if wanted[s.pid] {
				continue
			}
			if _, peer, ok := strings.Cut(s.name, "->"); ok {
				for _, owner := range owners[socketEndpoint(peer)] {
					if wanted[owner] {
						result[owner] = model.PeerConnected
					}
				}
			}
		}
	}
	for _, s := range sockets {
		if !wanted[s.pid] {
			continue
		}
		if s.state == "ESTABLISHED" || strings.Contains(s.name, "(ESTABLISHED)") {
			result[s.pid] = model.PeerConnected
			continue
		}
		if s.state == "LISTEN" || strings.Contains(s.name, "(LISTEN)") {
			result[s.pid] = max(result[s.pid], model.PeerListening)
			continue
		}
		_, peer, connected := strings.Cut(s.name, "->")
		if connected && socketEndpoint(peer) == "(none)" {
			continue
		}
		if connected {
			peerOwners := owners[socketEndpoint(peer)]
			external := len(peerOwners) == 0
			for _, owner := range peerOwners {
				if !wanted[owner] {
					external = true
				}
			}
			if external {
				// The unrestricted pass lists Unix sockets only, so a network
				// peer always stays connected.
				unixPeer := s.kind == "unix" || strings.HasPrefix(socketEndpoint(peer), "0x") || strings.HasPrefix(peer, "/")
				if unixPeer && len(peerOwners) == 0 && visible != nil && !visible[socketEndpoint(peer)] {
					// Another user's process, such as a system service.
					result[s.pid] = max(result[s.pid], model.PeerSystem)
				} else {
					result[s.pid] = model.PeerConnected
				}
			}
		} else if (s.kind == "unix" || strings.HasPrefix(s.name, "/")) && socketEndpoint(s.name) != "" {
			// A bound Unix socket with no connected endpoint is serving too.
			result[s.pid] = max(result[s.pid], model.PeerListening)
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

type lsofSocket struct {
	pid                       int32
	device, kind, name, state string
}

// endpoints returns the addresses a Unix socket is known by: its kernel
// address and any bound path.
func (s lsofSocket) endpoints() []string {
	if s.kind != "unix" && !strings.Contains(s.name, "->0x") && !strings.HasPrefix(s.name, "/") {
		return nil
	}
	local, _, _ := strings.Cut(s.name, "->")
	var endpoints []string
	for _, endpoint := range []string{s.device, local} {
		if endpoint = socketEndpoint(endpoint); endpoint != "" {
			endpoints = append(endpoints, endpoint)
		}
	}
	return endpoints
}

func parseLsofSockets(out []byte) []lsofSocket {
	var sockets []lsofSocket
	var pid int32
	var current lsofSocket
	flush := func() {
		if current.pid > 0 {
			sockets = append(sockets, current)
		}
		current = lsofSocket{pid: pid}
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
			current = lsofSocket{pid: pid}
		case 'f':
			flush()
		case 'd', 'D':
			// A Unix socket's kernel address; macOS lsof prints it as d.
			if strings.HasPrefix(line[1:], "0x") {
				current.device = line[1:]
			}
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
	return sockets
}
