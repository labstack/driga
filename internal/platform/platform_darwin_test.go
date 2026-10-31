package platform_test

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/shirou/gopsutil/v4/process"
)

func selfStart(t *testing.T) time.Time {
	t.Helper()
	p, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	ms, err := p.CreateTime()
	if err != nil {
		t.Fatal(err)
	}
	return time.UnixMilli(ms)
}

func TestProcsIncludesSelf(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	procs, err := platform.New().Procs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range procs {
		if p.PID == int32(os.Getpid()) {
			if !p.StartTime.Equal(selfStart(t)) || p.PPID != int32(os.Getppid()) || p.RSS == 0 || p.Exe == "" || p.Cmdline == "" || p.CPUSeconds < 0 || p.CPUPercent != 0 || p.Cwd != "" {
				t.Fatalf("incomplete self process: %+v", p)
			}
			return
		}
	}
	t.Fatal("own pid missing from process snapshot")
}

func TestAliveRejectsWrongStart(t *testing.T) {
	if platform.New().Alive(int32(os.Getpid()), selfStart(t).Add(-10*time.Second)) {
		t.Fatal("Alive accepted a different process start")
	}
}

func TestAliveAcceptsSelf(t *testing.T) {
	p := platform.New()
	start := selfStart(t)
	for _, delta := range []time.Duration{0, -time.Second, time.Second} {
		if !p.Alive(int32(os.Getpid()), start.Add(delta)) {
			t.Fatalf("Alive rejected self with start offset %v", delta)
		}
	}
	if p.Alive(-1, start) {
		t.Fatal("Alive accepted an invalid pid")
	}
}

func TestAliveUsesAsymmetricWindow(t *testing.T) {
	p, pid, create := platform.New(), int32(os.Getpid()), selfStart(t)
	if !p.Alive(pid, create.Add(30*time.Second)) {
		t.Fatal("Alive rejected own pid with registry written 30s after exec")
	}
	if p.Alive(pid, create.Add(-2*time.Second)) {
		t.Fatal("Alive accepted create time 2s after recorded start")
	}
}

func TestCwdsSelf(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pid := int32(os.Getpid())
	got, err := platform.New().Cwds(ctx, []int32{pid})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	want, err = filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatal(err)
	}
	if got[pid] != want {
		t.Fatalf("cwd = %q; want %q", got[pid], want)
	}
}

func fakeCommand(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return dir
}

func TestCwdsEmpty(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	got, err := platform.New().Cwds(context.Background(), nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("Cwds(nil) = %v, %v", got, err)
	}
}

func TestCwdsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := platform.New().Cwds(ctx, []int32{int32(os.Getpid())})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Cwds canceled error = %v", err)
	}
}

func TestExternalPeers(t *testing.T) {
	fakeCommand(t, "lsof", `[ "$#" -eq 8 ] && [ "$1" = -a ] && [ "$3" = -n ] && [ "$4" = -P ] && [ "$5" = -F ] && [ "$6" = pcdnDtT ] && [ "$7" = -p ] && [ "$8" = 10,11 ] || exit 2
[ "$2" = -i ] && exit 1
[ "$2" = -U ] || exit 2
printf 'p10\nf3\ntunix\nD0xa\nn->0xb\np11\nf3\ntunix\nD0xb\nn->0xa\n'
exit 1
`)
	prober, ok := platform.New().(platform.SocketProber)
	if !ok {
		t.Fatal("Darwin platform does not implement SocketProber")
	}
	got, err := prober.ExternalPeers(context.Background(), []int32{10, 11})
	if err != nil || len(got) != 2 || got[10] != model.PeerNone || got[11] != model.PeerNone {
		t.Fatalf("peers = %v, %v", got, err)
	}
}

func TestExternalPeersTimeout(t *testing.T) {
	fakeCommand(t, "lsof", "exec /bin/sleep 7\n")
	prober, ok := platform.New().(platform.SocketProber)
	if !ok {
		t.Fatal("Darwin platform does not implement SocketProber")
	}
	start := time.Now()
	_, err := prober.ExternalPeers(context.Background(), []int32{10})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 6*time.Second {
		t.Fatalf("error = %v after %s", err, time.Since(start))
	}
}

func TestExternalPeersListenerSelf(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	pid := int32(os.Getpid())
	prober := platform.New().(platform.SocketProber)
	peers, err := prober.ExternalPeers(context.Background(), []int32{pid})
	if err != nil || peers[pid] != model.PeerListening {
		t.Fatalf("own listener missing: %v, %v", peers, err)
	}
}

func TestMachine(t *testing.T) {
	fakeCommand(t, "memory_pressure", "printf 'System-wide memory free percentage: 34%%\\n'\n")
	before := time.Now()
	s, err := platform.New().Machine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.TS.Before(before) || s.TS.After(time.Now()) || s.MemTotal == 0 || s.MemUsed == 0 || s.DiskFree == 0 || s.Cores < 1 || s.PressureFree != 34 || s.SwapUsed > s.SwapTotal || s.Load1 < 0 || s.Load5 < 0 || s.Load15 < 0 {
		t.Fatalf("invalid machine sample: %+v", s)
	}
}

func TestMachinePressureFailure(t *testing.T) {
	for i, body := range []string{"exit 1\n", "printf 'invalid\\n'\n", "exec /bin/sleep 10\n"} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			fakeCommand(t, "memory_pressure", body)
			before := time.Now()
			s, err := platform.New().Machine(context.Background())
			if err != nil || s.MemTotal == 0 || s.PressureFree != 0 {
				t.Fatalf("Machine = %+v, %v", s, err)
			}
			if elapsed := time.Since(before); elapsed > 5*time.Second {
				t.Fatalf("pressure command was not bounded: %v", elapsed)
			}
		})
	}
}

func TestProcsRescanReusesIdentity(t *testing.T) {
	// Collectors rescan every few seconds; re-reading every process's argv and
	// environment each pass dominated a serving collector's allocation rate.
	ctx := context.Background()
	alloc := func(p platform.Platform) uint64 {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		if _, err := p.Procs(ctx); err != nil {
			t.Fatal(err)
		}
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}
	warm := platform.New()
	cold := alloc(warm)
	again := alloc(warm)
	t.Logf("first scan %d bytes, rescan %d bytes", cold, again)
	if again*3 > cold {
		t.Fatalf("rescan allocated %d bytes, first scan %d; want under a third", again, cold)
	}
}

func TestProcsRescanSeesExec(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.Command("/bin/sh", "-c", "read _; exec /bin/sleep 30")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	p := platform.New()
	find := func() string {
		procs, err := p.Procs(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, proc := range procs {
			if proc.PID == int32(cmd.Process.Pid) {
				return proc.Cmdline
			}
		}
		return ""
	}
	// argv is unreadable for a moment after spawn; an empty cmdline is not cached.
	deadline := time.Now().Add(5 * time.Second)
	for got := find(); !strings.Contains(got, "read _"); got = find() {
		if time.Now().After(deadline) {
			t.Fatalf("before exec cmdline = %q", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := stdin.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for got := find(); got != "/bin/sleep 30"; got = find() {
		if time.Now().After(deadline) {
			t.Fatalf("after exec cmdline = %q, want /bin/sleep 30", got)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestCwdsAllOwnProcessesQuickly(t *testing.T) {
	// Cleanup inspects every process the user owns; lsof batches took ~15 s for
	// 1,700 processes and hit the 10 s deadline.
	ctx := context.Background()
	p := platform.New()
	procs, err := p.Procs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pids := make([]int32, 0, len(procs))
	for _, proc := range procs {
		if proc.OwnerKnown && proc.OwnerUID == uint32(os.Getuid()) {
			pids = append(pids, proc.PID)
		}
	}
	started := time.Now()
	cwds, err := p.Cwds(ctx, pids)
	if err != nil {
		t.Fatalf("%d pids: %v", len(pids), err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("Cwds for %d pids took %v", len(pids), elapsed)
	}
	if wd, _ := os.Getwd(); cwds[int32(os.Getpid())] != wd {
		t.Fatalf("own cwd = %q, want %q", cwds[int32(os.Getpid())], wd)
	}
}

// If the unrestricted pass is incomplete, a client may be missing, so every
// pid reports connected rather than idle.
func TestResolvedPeersFailsClosed(t *testing.T) {
	fakeCommand(t, "lsof", `[ "$2" = -i ] && exit 1
[ "$1" = -U ] && { printf 'p10\nf3\ntunix\nd0xa\nn/private/tmp/broker.sock\n'; exit 1; }
exit 2
`)
	resolver, ok := platform.New().(platform.PeerResolver)
	if !ok {
		t.Fatal("Darwin platform does not implement PeerResolver")
	}
	got, err := resolver.ResolvedPeers(context.Background(), []int32{10, 11})
	if err != nil || got[10] != model.PeerConnected || got[11] != model.PeerConnected {
		t.Fatalf("peers = %v, %v", got, err)
	}
}
