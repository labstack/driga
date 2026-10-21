package platform

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func procFixture(t *testing.T) string {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink fixture; Windows watch tests require no Developer Mode or admin rights")
	}

	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("testdata/proc")); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{
		"10/fd/3": "socket:[201]", "11/fd/3": "socket:[202]",
		"11/fd/4": "socket:[101]", "11/fd/5": "socket:[101]",
		"11/fd/6": "pipe:[900]", "10/cwd": "/work/a b/ç", "11/cwd": "/work/child",
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestParseMeminfo(t *testing.T) {
	b, err := os.ReadFile("testdata/proc/meminfo")
	if err != nil {
		t.Fatal(err)
	}
	total, avail, swapTotal, swapFree := ParseMeminfo(b)
	if total != 4096*1024 || avail != 1024*1024 || swapTotal != 2048*1024 || swapFree != 1536*1024 {
		t.Fatalf("meminfo = %d %d %d %d", total, avail, swapTotal, swapFree)
	}
	for _, input := range []string{"", "MemTotal: nope kB\nMemAvailable: -1 kB\nSwapTotal: 18446744073709551615 kB\n", "MemTotal: 1 MB\n"} {
		a, b, c, d := ParseMeminfo([]byte(input))
		if a != 0 || b != 0 || c != 0 || d != 0 {
			t.Fatalf("invalid meminfo %q = %d %d %d %d", input, a, b, c, d)
		}
	}
}

func TestParsePSIPresentAndAbsent(t *testing.T) {
	for _, tt := range []struct {
		name, input      string
		present, invalid bool
	}{
		{"present", "some avg10=1.23 avg60=0.45 avg300=0.10 total=123\nfull avg10=0.00 avg60=0.00\n", true, false},
		{"absent", "", false, false},
		{"full only", "full avg10=0.00\n", false, false},
		{"malformed", "some avg10=nope\n", false, true},
		{"missing avg10", "some avg60=0.00\n", false, true},
		{"nan", "some avg10=NaN\n", false, true},
		{"infinite", "some avg10=+Inf\n", false, true},
		{"negative", "some avg10=-1\n", false, true},
		{"over 100", "some avg10=101\n", false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePSI([]byte(tt.input))
			if (err != nil) != tt.invalid || (got != nil) != tt.present || (got != nil && *got != 1.23) {
				t.Fatalf("ParsePSI = %v, %v", got, err)
			}
		})
	}
	// Missing PSI must also work through the file-reading path, as in containers.
	s, err := (procFS{root: procFixture(t)}).Machine(context.Background())
	if err != nil || s.PsiMemSome10 != nil {
		t.Fatalf("missing PSI = %+v, %v", s, err)
	}
}

func TestParseProcNetTCPAndUnix(t *testing.T) {
	wants := map[string]map[uint64]SockEntry{
		"tcp": {
			100: {Inode: 100, State: "0A", Local: "127.0.0.1:7411", Remote: "0.0.0.0:0", Listen: true},
			101: {Inode: 101, State: "01", Local: "10.0.0.1:4000", Remote: "10.0.0.2:443"},
			102: {Inode: 102, State: "01", Local: "127.0.0.1:5000", Remote: "127.0.0.1:5001"},
			103: {Inode: 103, State: "01", Local: "127.0.0.1:5001", Remote: "127.0.0.1:5000"},
		},
		"tcp6": {104: {Inode: 104, State: "01", Local: "[::1]:5000", Remote: "[2001:db8::1]:443"}},
		"udp":  {105: {Inode: 105, State: "07", Local: "0.0.0.0:5353", Remote: "0.0.0.0:0", Listen: true}},
		"udp6": {106: {Inode: 106, State: "01", Local: "[::1]:5000", Remote: "[::1]:5001"}},
		"unix": {
			200: {Inode: 200, State: "01", Local: "/tmp/service socket", Listen: true},
			201: {Inode: 201, State: "03"}, 202: {Inode: 202, State: "03"},
		},
	}
	for kind, want := range wants {
		t.Run(kind, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join("testdata/proc/net", kind))
			if err != nil {
				t.Fatal(err)
			}
			if got := ParseProcNet(b, kind); !reflect.DeepEqual(got, want) {
				t.Fatalf("ParseProcNet = %#v; want %#v", got, want)
			}
			if got := ParseProcNet([]byte("bad row\n0: invalid invalid 01 0 0 0 0 0 999\n"), kind); len(got) != 0 {
				t.Fatalf("malformed rows accepted: %v", got)
			}
		})
	}
}

func TestLinuxExternalPeers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink fixture; Windows watch tests require no Developer Mode or admin rights")
	}

	root := procFixture(t)
	fs := procFS{root: root, unixPeers: func(context.Context) map[uint64]uint64 {
		// A hand-written UNIX_DIAG_PEER snapshot for the socket pair.
		return map[uint64]uint64{201: 202, 202: 201}
	}}
	got, err := fs.ExternalPeers(context.Background(), []int32{10, 11, 99})
	want := map[int32]bool{10: false, 11: true, 99: false}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("peers = %v, %v; want %v", got, err, want)
	}
	got, err = fs.ExternalPeers(context.Background(), []int32{10})
	if err != nil || !got[10] {
		t.Fatalf("peer outside tree = %v, %v", got, err)
	}
	fs.unixPeers = nil
	got, err = fs.ExternalPeers(context.Background(), []int32{10, 11})
	if err != nil || !got[10] {
		t.Fatalf("unresolved Unix peer = %v, %v", got, err)
	}
	// Opposite TCP endpoints owned by the tree are internal; listeners always serve.
	for name, inode := range map[string]string{"10/fd/7": "102", "11/fd/7": "103"} {
		if err := os.Symlink("socket:["+inode+"]", filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(root, "10/fd/3")); err != nil {
		t.Fatal(err)
	}
	got, err = fs.ExternalPeers(context.Background(), []int32{10, 11})
	if err != nil || got[10] {
		t.Fatalf("internal TCP = %v, %v", got, err)
	}
	if err := os.Symlink("socket:[100]", filepath.Join(root, "10/fd/8")); err != nil {
		t.Fatal(err)
	}
	got, err = fs.ExternalPeers(context.Background(), []int32{10, 11})
	if err != nil || !got[10] {
		t.Fatalf("listener = %v, %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fs.ExternalPeers(ctx, []int32{10}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v", err)
	}
}

func TestLinuxCwds(t *testing.T) {
	root := procFixture(t)
	fs := procFS{root: root}
	got, err := fs.Cwds(context.Background(), []int32{10, 11, 99, -1})
	want := map[int32]string{10: "/work/a b/ç", 11: "/work/child"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("cwds = %v, %v", got, err)
	}
	if got := ReadCwd(root, 10); got != want[10] {
		t.Fatalf("ReadCwd = %q", got)
	}
	if got := ReadCwd(root, 99); got != "" {
		t.Fatalf("missing cwd = %q", got)
	}
	if got := SocketInodes(root, 11); !reflect.DeepEqual(got, []uint64{101, 202}) {
		t.Fatalf("inodes = %v", got)
	}
	if got := SocketInodes(root, 99); len(got) != 0 {
		t.Fatalf("missing inodes = %v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fs.Cwds(ctx, []int32{10}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v", err)
	}
}

func TestLinuxMachinePressureFree(t *testing.T) {
	root := procFixture(t)
	if err := os.MkdirAll(filepath.Join(root, "pressure"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pressure/memory"), []byte("some avg10=1.23 avg60=0.00 total=123\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fs := procFS{root: root}
	s, err := fs.Machine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.PressureFree != 25 || s.MemTotal != 4096*1024 || s.MemUsed != 3072*1024 || s.SwapUsed != 512*1024 || s.SwapTotal != 2048*1024 || s.PsiMemSome10 == nil || *s.PsiMemSome10 != 1.23 || s.Cores < 1 || s.DiskFree == 0 || s.TS.IsZero() {
		t.Fatalf("sample = %+v", s)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fs.Machine(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v", err)
	}
}

func TestParseUnixDiagPeer(t *testing.T) {
	// unix_diag_msg (16 bytes), then a UNIX_DIAG_PEER attribute (8 bytes).
	data := make([]byte, 24)
	data[0] = 1 // AF_UNIX
	binary.NativeEndian.PutUint32(data[4:8], 201)
	binary.NativeEndian.PutUint16(data[16:18], 8)
	binary.NativeEndian.PutUint16(data[18:20], 2)
	binary.NativeEndian.PutUint32(data[20:24], 202)
	inode, peer := parseUnixDiagPeer(data)
	if inode != 201 || peer != 202 {
		t.Fatalf("peer = %d -> %d", inode, peer)
	}
	for n := 0; n < len(data); n++ {
		_, peer := parseUnixDiagPeer(data[:n])
		if peer != 0 {
			t.Fatalf("truncated length %d: peer = %d", n, peer)
		}
	}
	data[16], data[17] = 0, 0
	if _, peer := parseUnixDiagPeer(data); peer != 0 {
		t.Fatalf("invalid attribute: %d", peer)
	}
}

func TestUserSubreaper(t *testing.T) {
	root := t.TempDir()
	write := func(pid, name, body string) {
		t.Helper()
		path := filepath.Join(root, pid, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("10", "comm", "systemd\n")
	write("10", "status", "Name:\tsystemd\nUid:\t1001\t1001\t1001\t1001\n")
	write("10", "cmdline", "/usr/lib/systemd/systemd\x00--user\x00")
	if pid, ok := UserSubreaper(root, 1001); !ok || pid != 10 {
		t.Fatalf("%d %v", pid, ok)
	}
	if pid, ok := UserSubreaper(root, 1002); ok || pid != 0 {
		t.Fatal(pid, ok)
	}
	for _, tc := range []struct{ name, body string }{
		{"cmdline", "systemd\x00--user-other\x00"},
		{"cmdline", "systemd\x00--system\x00"},
		{"comm", "other\n"},
		{"status", "Uid:\tbad\n"},
	} {
		t.Run(tc.body, func(t *testing.T) {
			path := filepath.Join(root, "10", tc.name)
			old, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			write("10", tc.name, tc.body)
			if pid, ok := UserSubreaper(root, 1001); ok || pid != 0 {
				t.Fatal(pid, ok)
			}
			if err := os.WriteFile(path, old, 0600); err != nil {
				t.Fatal(err)
			}
		})
	}
	if pid, ok := UserSubreaper(filepath.Join(root, "missing"), 1001); ok || pid != 0 {
		t.Fatal(pid, ok)
	}
}

func TestLinuxVMContainerWithoutPSI(t *testing.T) {
	root := "../../testdata/fixtures/mixed/heldout-v1/heldout/linux-vm-synthetic/proc"
	pid, ok := UserSubreaper(root, 1001)
	if !ok || pid != 700 {
		t.Fatal(pid, ok)
	}
	sample, err := (procFS{root: root}).Machine(context.Background())
	if err != nil || sample.PsiMemSome10 != nil || sample.PressureFree != 25 {
		t.Fatalf("%+v %v", sample, err)
	}
}
