package kill

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
)

type approvedSnapshotPlatform struct {
	platform.Platform
	procs []model.Proc
}

func TestSnapshotFixture(t *testing.T) {
	if os.Getenv("DRIGA_SNAPSHOT_FIXTURE") != "1" {
		return
	}
	fmt.Println("fixture ready")
	for {
		time.Sleep(time.Hour)
	}
}

func TestSnapshotOwnedChild(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestSnapshotFixture$")
	cmd.Env = append(os.Environ(), "DRIGA_SNAPSHOT_FIXTURE=1")
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	ready := make(chan bool, 1)
	go func() { s := bufio.NewScanner(pipe); ready <- s.Scan() && s.Text() == "fixture ready" }()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("fixture startup")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fixture startup timeout")
	}
	plat := platform.New()
	ps, e := plat.Procs(context.Background())
	if e != nil {
		t.Skipf("native process inspection unavailable: %v", e)
	}
	var target model.Proc
	for _, p := range ps {
		if p.PID == int32(cmd.Process.Pid) {
			target = p
			break
		}
	}
	if target.PID == 0 {
		t.Fatal("owned fixture not collected")
	}
	r, e := Snapshot(context.Background(), plat, []model.Proc{target}, nil, false, 100*time.Millisecond)
	if e != nil {
		t.Fatal(e)
	}
	if runtime.GOOS == "windows" && len(r.Survivors) == 1 {
		r, e = Snapshot(context.Background(), plat, r.Survivors, nil, true, 100*time.Millisecond)
	}
	if e != nil || len(r.Stopped) != 1 || len(r.Survivors) != 0 || len(r.Skipped) != 0 {
		t.Fatalf("owned child: %#v %v", r, e)
	}
}

func (p *approvedSnapshotPlatform) Procs(context.Context) ([]model.Proc, error) { return p.procs, nil }
func snapshotProc(pid int32) model.Proc {
	p := model.Proc{PID: pid, PPID: 2, StartTime: time.Unix(100, 123456000), OwnerKnown: true, OwnerUID: uint32(os.Getuid()), Exe: "/fixture/dummy", Cmdline: "dummy fixture", Argv: []string{"dummy", "fixture"}}
	if runtime.GOOS == "windows" {
		u, _ := user.Current()
		p.OwnerSID = u.Uid
	}
	return p
}

func TestProcessSnapshotIdentity(t *testing.T) {
	original := snapshotProc(400001)
	for name, change := range map[string]func(*model.Proc){"start": func(p *model.Proc) { p.StartTime = p.StartTime.Add(time.Nanosecond) }, "owner": func(p *model.Proc) { p.OwnerUID++; p.OwnerSID = "other" }, "unknown owner": func(p *model.Proc) { p.OwnerKnown = false }, "exe": func(p *model.Proc) { p.Exe = "/fixture/other" }, "argv": func(p *model.Proc) { p.Argv = []string{"dummy", "changed"} }, "cmdline": func(p *model.Proc) { p.Cmdline = "changed" }, "protected descendant": func(p *model.Proc) { p.PPID = 400002 }} {
		t.Run(name, func(t *testing.T) {
			live := original
			change(&live)
			plat := &approvedSnapshotPlatform{procs: []model.Proc{live, snapshotProc(400002)}}
			called := false
			r, e := snapshot(context.Background(), plat, []model.Proc{original}, []model.Proc{snapshotProc(400002)}, false, time.Millisecond, func(model.Proc, syscall.Signal) error { called = true; return nil })
			if e != nil || called || len(r.Skipped) != 1 || len(r.Vanished) != 0 || r.Reasons[original.PID] == "" {
				t.Fatalf("%#v %v signalled=%t", r, e, called)
			}
		})
	}
	plat := &approvedSnapshotPlatform{procs: []model.Proc{snapshotProc(400002)}}
	r, e := snapshot(context.Background(), plat, []model.Proc{original}, nil, false, time.Millisecond, func(model.Proc, syscall.Signal) error { t.Fatal("expanded to later match"); return nil })
	if e != nil || len(r.Vanished) != 1 {
		t.Fatalf("vanished: %#v %v", r, e)
	}
}

func TestSnapshotSignalResults(t *testing.T) {
	a, b, c, d := snapshotProc(400001), snapshotProc(400002), snapshotProc(400003), snapshotProc(400004)
	changed := c
	changed.Exe = "other"
	plat := &approvedSnapshotPlatform{procs: []model.Proc{a, changed, d}}
	var signals []syscall.Signal
	r, e := snapshot(context.Background(), plat, []model.Proc{a, b, c, d, a}, nil, false, time.Millisecond, func(p model.Proc, s syscall.Signal) error {
		signals = append(signals, s)
		if p.PID == a.PID {
			plat.procs = plat.procs[1:]
		}
		return nil
	})
	if e != nil || len(r.Stopped) != 1 || len(r.Vanished) != 1 || len(r.Skipped) != 1 || len(r.Survivors) != 1 || len(signals) != 2 || signals[0] != syscall.SIGTERM || signals[1] != syscall.SIGTERM {
		t.Fatalf("%#v %v signals=%v", r, e, signals)
	}
	// A separate force operation receives only the approved surviving identity.
	r, e = snapshot(context.Background(), plat, r.Survivors, nil, true, time.Millisecond, func(p model.Proc, s syscall.Signal) error {
		if p.PID != d.PID || s != syscall.SIGKILL {
			t.Fatal("force widened")
		}
		plat.procs = plat.procs[:1]
		return nil
	})
	if e != nil || len(r.Stopped) != 1 {
		t.Fatalf("force %#v %v", r, e)
	}
	plat.procs = []model.Proc{a}
	r, e = snapshot(context.Background(), plat, []model.Proc{a}, nil, false, time.Millisecond, func(model.Proc, syscall.Signal) error { return syscall.ESRCH })
	if e != nil || len(r.Vanished) != 1 {
		t.Fatalf("ESRCH %#v %v", r, e)
	}
	r, e = snapshot(context.Background(), plat, []model.Proc{a}, nil, false, time.Millisecond, func(model.Proc, syscall.Signal) error { return errors.New("denied") })
	if e == nil || len(r.Skipped) != 1 {
		t.Fatalf("signal error %#v %v", r, e)
	}
}

func TestSnapshotSignalRecheckOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		wantErr bool
	}{
		{"changed identity", errSnapshotIdentityChanged, false},
		{"wrapped changed identity", fmt.Errorf("identity recheck: %w", errSnapshotIdentityChanged), false},
		{"permission denied", syscall.EPERM, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stopped, vanished, skipped, survivor := snapshotProc(400001), snapshotProc(400002), snapshotProc(400003), snapshotProc(400004)
			plat := &approvedSnapshotPlatform{procs: []model.Proc{stopped, skipped, survivor}}
			r, err := snapshot(context.Background(), plat, []model.Proc{stopped, vanished, skipped, survivor}, nil, false, time.Millisecond, func(p model.Proc, sig syscall.Signal) error {
				if sig != syscall.SIGTERM {
					t.Fatalf("unexpected signal: %v", sig)
				}
				switch p.PID {
				case stopped.PID:
					plat.procs = plat.procs[1:]
				case skipped.PID:
					return tc.err
				case survivor.PID:
				default:
					t.Fatalf("unexpected signal target: %d", p.PID)
				}
				return nil
			})
			if (err != nil) != tc.wantErr || (tc.wantErr && !errors.Is(err, tc.err)) {
				t.Fatalf("execution error = %v, wantErr = %t", err, tc.wantErr)
			}
			for _, outcome := range []struct {
				name string
				got  []model.Proc
				want model.Proc
			}{{"stopped", r.Stopped, stopped}, {"vanished", r.Vanished, vanished}, {"skipped", r.Skipped, skipped}, {"survivors", r.Survivors, survivor}} {
				if len(outcome.got) != 1 || outcome.got[0].PID != outcome.want.PID {
					t.Fatalf("%s = %+v, want PID %d", outcome.name, outcome.got, outcome.want.PID)
				}
			}
			if r.Reasons[skipped.PID] != tc.err.Error() {
				t.Fatalf("skip reason = %q, want %q", r.Reasons[skipped.PID], tc.err.Error())
			}
		})
	}
}
