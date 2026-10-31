package platform

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"

	"slices"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/common"

	"github.com/labstack/driga/internal/model"
)

// ParsePSI extracts memory "some avg10"; an absent some line returns nil.
func ParsePSI(data []byte) (*float64, error) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "some" {
			continue
		}
		for _, field := range fields[1:] {
			if value, ok := strings.CutPrefix(field, "avg10="); ok {
				n, err := strconv.ParseFloat(value, 64)
				if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 100 {
					return nil, fmt.Errorf("invalid PSI avg10 %q", value)
				}
				return &n, nil
			}
		}
		return nil, errors.New("PSI some line missing avg10")
	}
	return nil, nil
}

// SockEntry describes an inode in a Linux /proc/net socket table.
type SockEntry struct {
	Inode         uint64
	State         string
	Local, Remote string
	Listen        bool
}

// ParseProcNet parses tcp, tcp6, udp, udp6, or unix tables, skipping invalid rows.
// Internet endpoints use IP:port notation; Unix Local is the optional bound name.
func ParseProcNet(table []byte, kind string) map[uint64]SockEntry {
	result := make(map[uint64]SockEntry)
	if kind != "tcp" && kind != "tcp6" && kind != "udp" && kind != "udp6" && kind != "unix" {
		return result
	}
	for _, line := range strings.Split(string(table), "\n") {
		f := strings.Fields(line)
		var s SockEntry
		if kind == "unix" {
			if len(f) < 7 || !strings.HasSuffix(f[0], ":") {
				continue
			}
			inode, err := strconv.ParseUint(f[6], 10, 64)
			flags, flagErr := strconv.ParseUint(f[3], 16, 32)
			state, stateErr := strconv.ParseUint(f[5], 16, 8)
			if err != nil || flagErr != nil || stateErr != nil || inode == 0 {
				continue
			}
			s = SockEntry{Inode: inode, State: fmt.Sprintf("%02X", state), Listen: flags&0x10000 != 0}
			// Consume the seven columns without splitting spaces in a bound name.
			rest := strings.TrimSpace(line)
			for range 7 {
				idx := strings.IndexAny(rest, " \t")
				if idx < 0 {
					rest = ""
					break
				}
				rest = strings.TrimLeft(rest[idx:], " \t")
			}
			s.Local = rest
		} else {
			if len(f) < 10 || !strings.HasSuffix(f[0], ":") {
				continue
			}
			inode, err := strconv.ParseUint(f[9], 10, 64)
			local, localOK := procEndpoint(f[1], strings.HasSuffix(kind, "6"))
			remote, remoteOK := procEndpoint(f[2], strings.HasSuffix(kind, "6"))
			state, stateErr := strconv.ParseUint(f[3], 16, 8)
			if err != nil || inode == 0 || !localOK || !remoteOK || stateErr != nil {
				continue
			}
			s = SockEntry{Inode: inode, State: fmt.Sprintf("%02X", state), Local: local, Remote: remote, Listen: state == 0x0a}
			// UDP has no LISTEN state: a bound, unconnected socket is serving.
			if strings.HasPrefix(kind, "udp") && state == 7 {
				_, port, _ := net.SplitHostPort(local)
				s.Listen = port != "0"
			}
		}
		result[s.Inode] = s
	}
	return result
}

func procEndpoint(value string, ipv6 bool) (string, bool) {
	addr, portHex, ok := strings.Cut(value, ":")
	length := 8
	if ipv6 {
		length = 32
	}
	if !ok || len(addr) != length || len(portHex) != 4 {
		return "", false
	}
	port, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return "", false
	}
	ip := make(net.IP, length/2)
	for offset := 0; offset < len(addr); offset += 8 {
		n, err := strconv.ParseUint(addr[offset:offset+8], 16, 32)
		if err != nil {
			return "", false
		}
		// procfs renders each address word in host byte order.
		binary.NativeEndian.PutUint32(ip[offset/2:], uint32(n))
	}
	return net.JoinHostPort(ip.String(), strconv.FormatUint(port, 10)), true
}

// parseUnixDiagPeer decodes a unix_diag_msg and its UNIX_DIAG_PEER attribute.
// Keeping this parser portable lets malformed kernel records be tested on macOS.
func parseUnixDiagPeer(data []byte) (inode, peer uint64) {
	if len(data) < 16 || data[0] != 1 { // AF_UNIX
		return 0, 0
	}
	order := binary.NativeEndian
	inode = uint64(order.Uint32(data[4:8]))
	for attrs := data[16:]; len(attrs) >= 4; {
		length := int(order.Uint16(attrs[:2]))
		kind := order.Uint16(attrs[2:4]) & 0x3fff
		if length < 4 || length > len(attrs) {
			return inode, 0
		}
		if kind == 2 && length == 8 { // UNIX_DIAG_PEER
			return inode, uint64(order.Uint32(attrs[4:8]))
		}
		aligned := (length + 3) &^ 3
		if aligned > len(attrs) {
			break
		}
		attrs = attrs[aligned:]
	}
	return inode, 0
}

// SocketInodes returns sorted, unique socket inodes from a process's fd links.
// Inaccessible or disappearing descriptors are skipped.
func SocketInodes(procRoot string, pid int32) []uint64 {
	if pid <= 0 {
		return nil
	}
	dir := filepath.Join(procRoot, strconv.FormatInt(int64(pid), 10), "fd")
	entries, _ := os.ReadDir(dir)
	var result []uint64
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		target, ok := strings.CutPrefix(target, "socket:[")
		if !ok {
			continue
		}
		target, ok = strings.CutSuffix(target, "]")
		if !ok {
			continue
		}
		if inode, err := strconv.ParseUint(target, 10, 64); err == nil && inode != 0 {
			result = append(result, inode)
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}

// ReadCwd reads a process's cwd link; inaccessible/missing processes return empty.
func ReadCwd(procRoot string, pid int32) string {
	if pid <= 0 {
		return ""
	}
	path, _ := os.Readlink(filepath.Join(procRoot, strconv.FormatInt(int64(pid), 10), "cwd"))
	return path
}

// procFS holds the portable file-reading portion of the Linux collector so
// synthetic proc trees can exercise it on every development platform.
type procFS struct {
	root      string
	unixPeers func(context.Context) map[uint64]uint64
}

func (p procFS) context(ctx context.Context) context.Context {
	env := make(common.EnvMap)
	if existing, ok := ctx.Value(common.EnvKey).(common.EnvMap); ok {
		for key, value := range existing {
			env[key] = value
		}
	}
	env[common.HostProcEnvKey] = p.root
	return context.WithValue(ctx, common.EnvKey, env)
}

func (p procFS) Cwds(ctx context.Context, pids []int32) (map[int32]string, error) {
	result := make(map[int32]string)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if cwd := ReadCwd(p.root, pid); cwd != "" {
			result[pid] = cwd
		}
	}
	return result, ctx.Err()
}

func (p procFS) ExternalPeers(ctx context.Context, pids []int32) (map[int32]model.PeerState, error) {
	result := make(map[int32]model.PeerState, len(pids))
	owned := make(map[uint64]bool)
	inodes := make(map[int32][]uint64, len(pids))
	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result[pid] = model.PeerNone
		inodes[pid] = SocketInodes(p.root, pid)
		for _, inode := range inodes[pid] {
			owned[inode] = true
		}
	}
	for _, kind := range []string{"tcp", "tcp6", "udp", "udp6", "unix"} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(filepath.Join(p.root, "net", kind))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("proc net %s: %w", kind, err)
		}
		table := ParseProcNet(data, kind)
		var peers map[uint64]uint64
		if kind == "unix" && p.unixPeers != nil {
			for inode, s := range table {
				if owned[inode] && s.State == "03" {
					peers = p.unixPeers(ctx)
					break
				}
			}
		}
		type endpointPair struct{ local, remote string }
		internal := make(map[endpointPair]bool)
		for inode, s := range table {
			if owned[inode] && s.State == "01" {
				internal[endpointPair{s.Local, s.Remote}] = true
			}
		}
		for pid, sockets := range inodes {
			for _, inode := range sockets {
				s, ok := table[inode]
				if !ok {
					continue
				}
				if s.Listen && strings.HasPrefix(kind, "udp") {
					// A bound UDP endpoint can exchange datagrams with anyone,
					// as on Windows, so it counts as connected.
					result[pid] = model.PeerConnected
				} else if s.Listen {
					result[pid] = max(result[pid], model.PeerListening)
				} else if kind == "unix" {
					if s.State == "03" && !owned[peers[inode]] {
						result[pid] = model.PeerConnected
					}
				} else if s.State == "01" && !internal[endpointPair{s.Remote, s.Local}] {
					result[pid] = model.PeerConnected
				}
			}
		}
	}
	return result, ctx.Err()
}
