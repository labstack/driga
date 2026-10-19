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
`)
	pids := []int32{10, 11, 12, 13, 14, 15, 16, 17, 18, 19}
	want := map[int32]bool{10: true, 11: false, 12: false, 13: false, 14: true, 15: true, 16: true, 17: true, 18: false, 19: true}
	if got := platform.ParseLsofPeers(out, pids); !reflect.DeepEqual(got, want) {
		t.Fatalf("peers = %v; want %v", got, want)
	}
	// A peer whose record is present but is outside the target set is external.
	if got := platform.ParseLsofPeers(out, []int32{11}); !got[11] || len(got) != 1 {
		t.Fatalf("peer outside pid set: %v", got)
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
	f, err := platform.LoadFake(dir)
	if err != nil {
		t.Fatal(err)
	}
	var prober platform.SocketProber = f
	got, err := prober.ExternalPeers(context.Background(), []int32{10, 11})
	if err != nil || !got[10] || got[11] {
		t.Fatalf("peers = %v, %v", got, err)
	}
	got, err = prober.ExternalPeers(context.Background(), []int32{11})
	if err != nil || !got[11] {
		t.Fatalf("outside peer = %v, %v", got, err)
	}
}

func TestFakeAliveUsesExactFixtureIdentity(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "procs.json"), []byte(`[{"pid":10,"started_at":"2026-10-18T00:00:00Z"}]`), 0600); err != nil {
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
