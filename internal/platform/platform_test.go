package platform_test

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
)

func TestParseLsofPeers(t *testing.T) {
	// Hand-written lsof field records: D identifies a Unix socket's local
	// address; n supplies its peer; T supplies the TCP state.
	out := []byte(`p10
cbroker
f3
tunix
D0x000a
n->0x000b
f4
tunix
D0x000c
n->0x9999
p11
cworker
f3
tunix
D0x000b
n->0x000a
p12
cworker
f3
tunix
D0x000d
n/private/tmp/worker.sock->/private/tmp/child.sock
p13
cchild
f3
tunix
D0x000e
n/private/tmp/child.sock->/private/tmp/worker.sock
p14
cdaemon
f3
tIPv4
n127.0.0.1:4000->127.0.0.1:5000
TST=ESTABLISHED
p15
cserver
f3
tIPv6
n*:6000
TST=LISTEN
p16
cserver
f3
tunix
n/private/tmp/service.sock
p17
cclient
f3
tunix
n->/private/tmp/outside.sock
p18
cidle
f3
tIPv4
n127.0.0.1:9000
TST=CLOSED
p19
cclient
f3
n127.0.0.1:4001->127.0.0.1:5001 (ESTABLISHED)
p20
cstdio
f0
tunix
D0x7a825c5d47b4858d
n->(none)
p21
cbroker
f12
tunix
D0x00f1
n->0x00f2
f17
tunix
D0x00f3
n/private/tmp/cxc/broker.sock
p22
capp-server
f0
tunix
D0x00f2
n->0x00f1
`)
	none, listening, connected := model.PeerNone, model.PeerListening, model.PeerConnected
	pids := []int32{10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22}
	want := map[int32]model.PeerState{10: connected, 11: none, 12: none, 13: none, 14: connected, 15: listening, 16: listening, 17: connected, 18: none, 19: connected,
		// A peer that closed shows as (none); a broker whose only client is its
		// own child just listens.
		20: none, 21: listening, 22: none}
	if got := platform.ParseLsofPeers(out, pids); !reflect.DeepEqual(got, want) {
		t.Fatalf("peers = %v; want %v", got, want)
	}
	// A peer whose record is present but is outside the target set is external.
	if got := platform.ParseLsofPeers(out, []int32{11}); got[11] != connected || len(got) != 1 {
		t.Fatalf("peer outside pid set: %v", got)
	}
	// The broker's child connection leaves the tree when the child is not in it.
	if got := platform.ParseLsofPeers(out, []int32{21}); got[21] != connected {
		t.Fatalf("broker client outside pid set: %v", got)
	}
}

// Records in the shape macOS lsof -FpcdnDtT prints, taken from a live Codex
// plugin broker: d carries each Unix socket's own address, and a connection the
// broker accepted shows only the bound path, so only the client's side names it.
func TestParseLsofPeersAllMacOSBroker(t *testing.T) {
	tree := []byte(`p98021
cnode
f12
tunix
d0xe0d058e2c7c8edfd
n->0x46e036a0d862d8ac
f17
tunix
d0x98e16ac011dfaa79
n/private/tmp/cxc/broker.sock
f18
tunix
d0xa2e57851b5fd884c
n/private/tmp/cxc/broker.sock
p98025
ccodex
f0
tunix
d0x46e036a0d862d8ac
n->0xe0d058e2c7c8edfd
f34
tunix
d0x7a4edd3e6fd350f
n->0x4abc21701ce138cc
`)
	client := []byte(`p99984
cnc
f3
tunix
d0x7633b400310f9ee
n->0xa2e57851b5fd884c
`)
	pids := []int32{98021, 98025}
	// Without the unrestricted pass, an outside peer of unknown owner counts.
	strict := platform.ParseLsofPeers(tree, pids)
	if want := map[int32]model.PeerState{98021: model.PeerListening, 98025: model.PeerConnected}; !reflect.DeepEqual(strict, want) {
		t.Fatalf("strict = %v; want %v", strict, want)
	}
	// No other process names 0x4abc...: it belongs to another user's service.
	idle := platform.ParseLsofPeersAll(tree, pids, tree)
	if want := map[int32]model.PeerState{98021: model.PeerListening, 98025: model.PeerSystem}; !reflect.DeepEqual(idle, want) {
		t.Fatalf("no client = %v; want %v", idle, want)
	}
	busy := platform.ParseLsofPeersAll(tree, pids, append(append([]byte(nil), tree...), client...))
	if busy[98021] != model.PeerConnected {
		t.Fatalf("accepted client missed: %v", busy)
	}
	// The unrestricted pass covers Unix sockets only; network peers it cannot
	// list stay connected, whatever their TCP state.
	network := []byte(`p98025
ccodex
f40
tIPv4
n10.0.0.2:5353->1.1.1.1:53
f41
tIPv4
n10.0.0.2:50000->140.82.112.3:443
TST=CLOSE_WAIT
`)
	if got := platform.ParseLsofPeersAll(network, []int32{98025}, tree); got[98025] != model.PeerConnected {
		t.Fatalf("network peer downgraded: %v", got)
	}
}

func TestFakeExternalPeers(t *testing.T) {
	dir := t.TempDir()
	for name, contents := range map[string]string{
		"procs.json":   `[{"pid":10,"started_at":"2026-10-18T00:00:00Z"},{"pid":11,"started_at":"2026-10-18T00:00:00Z"}]`,
		"sockets.json": `{"10":true}`,
		"peers.json":   `{"11":[10]}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "machine.json"), []byte(`{"os":"linux"}`), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := platform.LoadFake(dir)
	if err != nil {
		t.Fatal(err)
	}
	var prober platform.SocketProber = f
	got, err := prober.ExternalPeers(context.Background(), []int32{10, 11})
	if err != nil || got[10] != model.PeerConnected || got[11] != model.PeerNone {
		t.Fatalf("peers = %v, %v", got, err)
	}
	got, err = prober.ExternalPeers(context.Background(), []int32{11})
	if err != nil || got[11] != model.PeerConnected {
		t.Fatalf("outside peer = %v, %v", got, err)
	}
}

func TestFakeAliveUsesExactFixtureIdentity(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "procs.json"), []byte(`[{"pid":10,"started_at":"2026-10-18T00:00:00Z"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "machine.json"), []byte(`{"os":"linux"}`), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := platform.LoadFake(dir)
	if err != nil {
		t.Fatal(err)
	}
	create := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	if !f.Alive(10, create) || f.Alive(10, create.Add(time.Millisecond)) || f.Alive(10, create.Add(-time.Millisecond)) {
		t.Fatal("fake Alive must preserve exact fixture identity")
	}
}

func TestParsePressure(t *testing.T) {
	for _, tt := range []struct {
		name, out string
		want      float64
		wantErr   bool
	}{
		{"normal", "other statistics\nSystem-wide memory free percentage: 34%\n", 34, false},
		{"decimal", "System-wide memory free percentage: 50.5%\n", 50.5, false},
		{"zero", "System-wide memory free percentage: 0%", 0, false},
		{"full", "System-wide memory free percentage: 100%", 100, false},
		{"missing", "unrelated: 34%", 0, true},
		{"empty", "", 0, true},
		{"no percent", "System-wide memory free percentage: 34", 0, true},
		{"invalid", "System-wide memory free percentage: nope%", 0, true},
		{"negative", "System-wide memory free percentage: -1%", 0, true},
		{"too large", "System-wide memory free percentage: 101%", 0, true},
		{"nan", "System-wide memory free percentage: NaN%", 0, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := platform.ParsePressure([]byte(tt.out))
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("ParsePressure = %v, %v; want %v, error %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestParseLsofCwd(t *testing.T) {
	for _, tt := range []struct {
		name, out string
		want      map[int32]string
	}{
		{"normal", "p12\nfcwd\nn/a b/ç\np13\nfcwd\nn/x\n", map[int32]string{12: "/a b/ç", 13: "/x"}},
		{"empty", "", map[int32]string{}},
		{"malformed", "n/orphan\np12\nn/ok\npbad\nn/wrong\np-1\nn/negative\np2147483648\nn/overflow\np13\n", map[int32]string{12: "/ok"}},
		{"preserve spaces", "p12\nn/a path  \n", map[int32]string{12: "/a path  "}},
		{"no final newline", "p12\nn/x", map[int32]string{12: "/x"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := platform.ParseLsofCwd([]byte(tt.out)); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseLsofCwd = %#v; want %#v", got, tt.want)
			}
		})
	}
}

func TestCPUPercent(t *testing.T) {
	start := time.Unix(100, 0)
	prev := model.Proc{PID: 12, StartTime: start, CPUSeconds: 2}
	for _, tt := range []struct {
		name string
		cur  model.Proc
		dt   time.Duration
		want float64
	}{
		{"normal", model.Proc{PID: 12, StartTime: start, CPUSeconds: 3.5}, 3 * time.Second, 50},
		{"multicore", model.Proc{PID: 12, StartTime: start, CPUSeconds: 8}, 3 * time.Second, 200},
		{"zero dt", model.Proc{PID: 12, StartTime: start, CPUSeconds: 3.5}, 0, 0},
		{"negative dt", model.Proc{PID: 12, StartTime: start, CPUSeconds: 3.5}, -time.Second, 0},
		{"pid reused", model.Proc{PID: 12, StartTime: start.Add(time.Millisecond), CPUSeconds: 3.5}, time.Second, 0},
		{"different pid", model.Proc{PID: 13, StartTime: start, CPUSeconds: 3.5}, time.Second, 0},
		{"backwards", model.Proc{PID: 12, StartTime: start, CPUSeconds: 1}, time.Second, 0},
		{"unchanged", prev, time.Second, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := platform.CPUPercent(prev, tt.cur, tt.dt); math.Abs(got-tt.want) > 1e-9 {
				t.Fatalf("CPUPercent = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestFakeRequiresSourceOS(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "procs.json"), []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.LoadFake(dir); err == nil {
		t.Fatal("missing machine.json accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "machine.json"), []byte(`{"cores":2}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.LoadFake(dir); err == nil {
		t.Fatal("missing source OS accepted")
	}
}
