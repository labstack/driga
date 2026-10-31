package platform

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"golang.org/x/sys/unix"
)

// Hand-built struct socket_fdinfo records at the SDK offsets.
func TestDecodeSocketFdInfo(t *testing.T) {
	unixRecord := make([]byte, sizeofSocketFdInfo)
	binary.LittleEndian.PutUint32(unixRecord[offSocketKind:], socketKindUn)
	binary.LittleEndian.PutUint64(unixRecord[offSocketPcb:], 0xe0d058e2c7c8edfd)
	binary.LittleEndian.PutUint64(unixRecord[offUnConnPcb:], 0x46e036a0d862d8ac)
	path := "/private/tmp/cxc/broker.sock"
	unixRecord[offUnAddr] = byte(2 + len(path) + 1)
	unixRecord[offUnAddr+1] = syscall.AF_UNIX
	copy(unixRecord[offUnAddr+2:], path)
	got, ok := decodeSocketFdInfo(7, unixRecord)
	if want := (lsofSocket{pid: 7, kind: "unix", device: "0xe0d058e2c7c8edfd", name: path + "->0x46e036a0d862d8ac"}); !ok || got != want {
		t.Fatalf("unix = %+v, %v; want %+v", got, ok, want)
	}

	tcp := make([]byte, sizeofSocketFdInfo)
	binary.LittleEndian.PutUint32(tcp[offSocketKind:], socketKindTCP)
	tcp[offInVflag] = 1
	copy(tcp[offInLaddr+12:], []byte{127, 0, 0, 1})
	copy(tcp[offInFaddr+12:], []byte{10, 0, 0, 2})
	binary.BigEndian.PutUint16(tcp[offInLport:], 4000)
	binary.BigEndian.PutUint16(tcp[offInFport:], 443)
	binary.LittleEndian.PutUint32(tcp[offTCPState:], 5)
	got, ok = decodeSocketFdInfo(8, tcp)
	if want := (lsofSocket{pid: 8, kind: "IPv4", name: "127.0.0.1:4000->10.0.0.2:443", state: "CLOSE_WAIT"}); !ok || got != want {
		t.Fatalf("tcp = %+v, %v; want %+v", got, ok, want)
	}

	other := make([]byte, sizeofSocketFdInfo)
	binary.LittleEndian.PutUint32(other[offSocketKind:], 9)
	if _, ok := decodeSocketFdInfo(9, other); ok {
		t.Fatal("unknown socket kind decoded")
	}
}

// A listener with a client in another process, read from the live kernel: the
// accepted connection names its client, and only the resolved check sees that
// the client is outside the tree when the accepted side is gone.
func TestProcSocketsLiveListener(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "drg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "s.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		if c, err := listener.Accept(); err == nil {
			accepted <- c
		}
	}()
	self := int32(os.Getpid())
	before, err := procSockets(self)
	if err != nil {
		t.Fatal(err)
	}
	var listening *lsofSocket
	for i, s := range before {
		if s.kind == "unix" && s.name == path {
			listening = &before[i]
		}
	}
	if listening == nil {
		t.Fatalf("listener %s not found in %+v", path, before)
	}

	client := exec.Command("/usr/bin/nc", "-U", path)
	stdin, err := client.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(); err != nil {
		t.Skipf("nc unavailable: %v", err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = client.Process.Kill(); _ = client.Wait() })
	var conn net.Conn
	select {
	case conn = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("client did not connect")
	}
	t.Cleanup(func() { _ = conn.Close() })
	clientPID := int32(client.Process.Pid)
	clientSockets, err := procSockets(clientPID)
	if err != nil || len(clientSockets) == 0 {
		t.Fatalf("client sockets = %+v, %v", clientSockets, err)
	}
	clientAddr := clientSockets[0].device
	after, err := procSockets(self)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(after, func(s lsofSocket) bool { return s.name == path+"->"+clientAddr }) {
		t.Fatalf("accepted connection does not name client %s: %+v", clientAddr, after)
	}

	ctx := context.Background()
	p := darwinPlatform{}
	if got, err := p.ResolvedPeers(ctx, []int32{self}); err != nil || got[self] != model.PeerConnected {
		t.Fatalf("listener with outside client = %v, %v", got, err)
	}
	if got, err := p.ExternalPeers(ctx, []int32{self}); err != nil || got[self] != model.PeerConnected {
		t.Fatalf("strict listener with outside client = %v, %v", got, err)
	}
	// With the client inside the tree, the connection is internal.
	if got, err := p.ResolvedPeers(ctx, []int32{self, clientPID}); err != nil || got[clientPID] != model.PeerNone {
		t.Fatalf("client inside tree = %v, %v", got, err)
	}
}

// libproc must list the same Unix sockets, with the same addresses and
// peers, as lsof does for this process.
func TestProcSocketsMatchLsof(t *testing.T) {
	a, b, err := socketPair()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(a); _ = unix.Close(b) })
	self := int32(os.Getpid())
	out, err := exec.Command("lsof", "-a", "-U", "-n", "-P", "-F", "pcdnDtT", "-p", strconv.Itoa(int(self))).Output()
	if err != nil {
		t.Skipf("lsof unavailable: %v", err)
	}
	fromLsof := map[string]string{}
	for _, s := range parseLsofSockets(out) {
		// lsof's own process records carry no socket address.
		if s.device == "" {
			continue
		}
		_, peer, _ := strings.Cut(s.name, "->")
		fromLsof[socketEndpoint(s.device)] = socketEndpoint(peer)
	}
	sockets, err := procSockets(self)
	if err != nil {
		t.Fatal(err)
	}
	fromProc := map[string]string{}
	for _, s := range sockets {
		if s.kind == "unix" {
			_, peer, _ := strings.Cut(s.name, "->")
			fromProc[socketEndpoint(s.device)] = socketEndpoint(peer)
		}
	}
	if len(fromProc) < 2 {
		t.Fatalf("socket pair missing: %v", fromProc)
	}
	for addr, peer := range fromLsof {
		got, ok := fromProc[addr]
		if !ok || (peer != "" && peer != "(none)" && got != peer) {
			t.Fatalf("lsof %s->%s; libproc %q (%v); all %v", addr, peer, got, ok, fromProc)
		}
	}
}

func socketPair() (int, int, error) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return 0, 0, err
	}
	return fds[0], fds[1], nil
}

// An exited process reports no sockets; reading it is not an error.
func TestTreeSocketsSkipsExited(t *testing.T) {
	cmd := exec.Command("/usr/bin/true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	gone := int32(cmd.Process.Pid)
	if got, err := (darwinPlatform{}).ExternalPeers(context.Background(), []int32{gone}); err != nil || got[gone] != model.PeerNone {
		t.Fatalf("exited pid = %v, %v", got, err)
	}
}

// Another user's processes are unreadable without root; they must not make
// the scan incomplete.
func TestAllUnixSocketsSkipsOtherUsers(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads every process")
	}
	sockets, complete, err := allUnixSockets(context.Background())
	if err != nil || !complete {
		t.Fatalf("scan: %d sockets, complete=%v, %v", len(sockets), complete, err)
	}
	if !sameUser(int32(os.Getpid()), uint32(os.Getuid())) || sameUser(1, uint32(os.Getuid())) {
		t.Fatal("process owner lookup")
	}
}
