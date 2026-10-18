package platform_test

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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

func TestCwdsBatchesAndPartialOutput(t *testing.T) {
	dir := fakeCommand(t, "lsof", `[ "$#" -eq 6 ] && [ "$1" = -a ] && [ "$2" = -d ] && [ "$3" = cwd ] && [ "$4" = -Fpn ] && [ "$5" = -p ] || exit 2
printf '%s\n' "$6" >> "$LSOF_CALLS"
IFS=,
for pid in $6; do
  printf 'p%s\nfcwd\nn/a b/ç\n' "$pid"
done
exit 1
`)
	log := filepath.Join(dir, "calls")
	t.Setenv("LSOF_CALLS", log)
	pids := make([]int32, 401)
	for i := range pids {
		pids[i] = int32(i + 1)
	}
	got, err := platform.New().Cwds(context.Background(), pids)
	if err != nil {
		t.Fatal(err)
	}
	for _, pid := range pids {
		if got[pid] != "/a b/ç" {
			t.Fatalf("cwd for %d = %q", pid, got[pid])
		}
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	calls := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(calls) != 3 {
		t.Fatalf("got %d calls; want 3", len(calls))
	}
	for _, call := range calls {
		if n := len(strings.Split(call, ",")); n > 200 {
			t.Fatalf("batch contains %d pids", n)
		}
	}
}

func TestCwdsEmpty(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	got, err := platform.New().Cwds(context.Background(), nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("Cwds(nil) = %v, %v", got, err)
	}
}

func TestCwdsCommandError(t *testing.T) {
	fakeCommand(t, "lsof", "printf 'p12\\nn/partial\\n'\nexit 2\n")
	got, err := platform.New().Cwds(context.Background(), []int32{12})
	if err == nil || got[12] != "/partial" {
		t.Fatalf("Cwds = %v, %v; want partial output and error", got, err)
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

func TestCwdsTimeout(t *testing.T) {
	// exec keeps cancellation confined to the test's own command process.
	fakeCommand(t, "lsof", "printf 'p12\\nn/partial\\n'\nexec /bin/sleep 12\n")
	start := time.Now()
	got, err := platform.New().Cwds(context.Background(), []int32{12})
	if !errors.Is(err, context.DeadlineExceeded) || got[12] != "/partial" || time.Since(start) > 11*time.Second {
		t.Fatalf("Cwds = %v, %v after %s; want partial output and 10s timeout", got, err, time.Since(start))
	}
}

func TestExternalPeers(t *testing.T) {
	fakeCommand(t, "lsof", `[ "$#" -eq 8 ] && [ "$1" = -a ] && [ "$3" = -n ] && [ "$4" = -P ] && [ "$5" = -F ] && [ "$6" = pcnDtT ] && [ "$7" = -p ] && [ "$8" = 10,11 ] || exit 2
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
	if err != nil || len(got) != 2 || got[10] || got[11] {
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
	if err != nil || !peers[pid] {
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
