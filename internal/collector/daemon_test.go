package collector

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/store"
)

func TestRunExclusiveLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission-bit contract; Windows uses ACLs")
	}

	d, _, _, a := setup(t)
	d.Owner = Owner{Kind: "service", Version: "test"}
	dir := config.DataDir()
	ready := make(chan struct{})
	a.collect = func(ctx context.Context) { close(ready); <-ctx.Done() }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, d, dir) }()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("collector never started")
	}
	owner, held, err := ReadOwner(dir)
	if err != nil || !held || owner.Kind != "service" || owner.Version != "test" || owner.PID != os.Getpid() {
		t.Fatalf("run owner: %+v, %v, %v", owner, held, err)
	}
	err = Run(context.Background(), d, dir)
	if !errors.Is(err, ErrAlreadyRunning) || !strings.Contains(err.Error(), "another driga collector is running (service, pid ") {
		t.Fatalf("lock: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "driga.lock"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("lock mode: %v %v", info, err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not stop collector")
	}
	// A stopped collector releases the lock without removing the lock inode.
	lock, err := Lock(dir, Owner{Kind: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
}

func TestCadenceFastWithHeartbeat(t *testing.T) {
	fast := cadence(time.Second)
	idle := cadence(5 * time.Second)
	if fast.Procs != 5*time.Second || fast.Git != 30*time.Second || fast.Machine != 10*time.Second || fast.GitFull != 5*time.Minute || fast.Cwds != time.Minute {
		t.Fatalf("fast: %+v", fast)
	}
	if idle.Procs != time.Minute || idle.Git != 5*time.Minute || idle.Machine != time.Minute || idle.GitFull != 5*time.Minute || idle.Cwds != time.Minute {
		t.Fatalf("idle: %+v", idle)
	}
	if Cadence(time.Hour) != idle {
		t.Fatal("exported cadence disagrees")
	}
}

func TestGapRecordedAfterDowntime(t *testing.T) {
	d, _, _, a := setup(t)
	dir := config.DataDir()
	old := d.Now().Add(-10 * time.Minute)
	st, err := store.Open(filepath.Join(dir, "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMeta("last_pass_at", old.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	st.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.collect = func(context.Context) { cancel() }
	if err := Run(ctx, d, dir); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(filepath.Join(dir, "driga.db"), store.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	gaps, err := st.Gaps(time.Time{})
	if err != nil || len(gaps) != 1 || !gaps[0].From.Equal(old) || !gaps[0].To.Equal(d.Now()) {
		t.Fatalf("gap: %+v %v", gaps, err)
	}
}

func TestDaemonSchedule(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	s := schedule{}
	p := s.plan(now, Cadence(time.Hour))
	if !p.Procs || !p.Machine || !p.Git || !p.GitFull || !p.Cwds {
		t.Fatalf("first plan: %+v", p)
	}
	s.advance(p, now)
	p = s.plan(now.Add(5*time.Second), Cadence(time.Second))
	if !p.Procs || p.Machine || p.Git || p.Cwds {
		t.Fatalf("viewer plan: %+v", p)
	}
	s.advance(p, now.Add(5*time.Second))
	p = s.plan(now.Add(30*time.Second), Cadence(time.Second))
	if !p.Git || !p.Machine || p.GitFull || p.Cwds {
		t.Fatalf("fast git/machine: %+v", p)
	}
	s.advance(p, now.Add(30*time.Second))
	p = s.plan(now.Add(5*time.Minute), Cadence(time.Hour))
	if !p.GitFull || !p.Git || !p.Cwds {
		t.Fatalf("full git deadline: %+v", p)
	}
	if wait := s.wait(now.Add(31*time.Second), Cadence(time.Hour)); wait > time.Second {
		t.Fatalf("heartbeat wake too late: %v", wait)
	}
}

// Running reads the PID the lock holder wrote; it never takes the lock, so it
// cannot make a collector that is starting at the same moment fail.
func TestRunningReadsLockOwner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix true executable fixture")
	}

	dir := t.TempDir()
	if Running(dir) {
		t.Fatal("no lock file reported running")
	}
	f, err := Lock(dir, Owner{Kind: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if !Running(dir) {
		t.Fatal("held lock not reported running")
	}
	f.Close()
	// A stale PID from an exited process is not running.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(Owner{PID: cmd.Process.Pid, Kind: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "driga.lock"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if Running(dir) {
		t.Fatal("exited owner reported running")
	}
}
