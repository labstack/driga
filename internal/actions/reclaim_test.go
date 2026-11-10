package actions

import (
	"context"
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/kill"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/reclaim"
	"github.com/labstack/driga/internal/view"
)

func TestExecutePersistsCancelledOutcome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DRIGA_DATA_DIR", dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := (KillOptions{}).Execute(ctx, Environment{Stdout: io.Discard}, config.Config{}, nil, []model.Orphan{{ID: "tree-1", MachineID: "host-1", RootPID: 42}})
	if err == nil || len(report.Outcomes) != 1 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	history, err := reclaim.Read(dir, 10, "process")
	if err != nil || len(history.Entries) != 1 {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	entry := history.Entries[0]
	if entry.ID != "tree-1" || entry.MachineID != "host-1" || entry.Status != "cancelled" || entry.At.IsZero() {
		t.Fatal(entry)
	}
}

// Alive always returns false, so these tests cannot reach a native signal.
type outputFailurePlatform struct {
	platform.Platform
	procs             []model.Proc
	final             []model.Proc
	finalErr          error
	refuse            bool
	calls, aliveCalls int
	observed          bool
}

func (p *outputFailurePlatform) Procs(ctx context.Context) ([]model.Proc, error) {
	p.calls++
	if _, final := ctx.Deadline(); final {
		p.observed = true
		return p.final, p.finalErr
	}
	if p.refuse && p.calls >= 3 {
		return nil, nil
	}
	return p.procs, nil
}
func (p *outputFailurePlatform) Alive(int32, time.Time) bool {
	p.aliveCalls++
	return false
}

// The synthetic final table is exhaustive; no native metadata is consulted.
func (p *outputFailurePlatform) ObserveProcess(_ context.Context, pid int32) (model.Proc, bool, error) {
	for _, proc := range p.final {
		if proc.PID == pid {
			return proc, true, nil
		}
	}
	return model.Proc{}, false, p.finalErr
}
func (p *outputFailurePlatform) ExternalPeers(_ context.Context, pids []int32) (map[int32]model.PeerState, error) {
	peers := make(map[int32]model.PeerState)
	for _, pid := range pids {
		peers[pid] = model.PeerNone
	}
	return peers, nil
}

type actionOutputWriter struct {
	fail func() bool
	err  error
}

func (w actionOutputWriter) Write(b []byte) (int, error) {
	if w.fail() {
		return 0, w.err
	}
	return len(b), nil
}

func TestKillOutputFailurePreservesObservedOutcome(t *testing.T) {
	for _, want := range []string{"completed", "pending", "partial", "unknown", "refused", "failed"} {
		t.Run(want, func(t *testing.T) {
			early := want == "failed"
			dir := t.TempDir()
			t.Setenv("DRIGA_DATA_DIR", dir)
			proc := model.Proc{PID: 123456789, PPID: 1, StartTime: time.Now().Add(-time.Hour), RSS: 128}
			proc.OwnerKnown, proc.OwnerUID = true, uint32(os.Getuid())
			if runtime.GOOS == "windows" {
				u, err := user.Current()
				if err != nil {
					t.Fatal(err)
				}
				proc.OwnerSID = u.Uid
			}
			proc.Exe, proc.Cmdline, proc.Argv = "/fixture/helper", "helper", []string{"helper"}
			p := &outputFailurePlatform{procs: []model.Proc{proc}}
			pids := []int32{proc.PID}
			switch want {
			case "pending":
				p.final = p.procs
			case "partial":
				child := proc
				child.PID, child.PPID, child.RSS = proc.PID+1, proc.PID, 64
				p.procs = append(p.procs, child)
				p.final = []model.Proc{child}
				pids = append(pids, child.PID)
			case "unknown":
				p.finalErr = context.Canceled
			case "refused":
				p.refuse = true
			}
			outputErr := errors.New("output unavailable")
			confirmed := false
			env := Environment{Stdout: actionOutputWriter{fail: func() bool { return early || p.observed || p.refuse && p.calls >= 3 }, err: outputErr}, Platform: func() platform.Platform { return p }, VerifyWindow: time.Nanosecond, Confirm: func(string) error { confirmed = true; return nil }}
			report, err := (KillOptions{}).Execute(context.Background(), env, config.Config{}, p.procs, []model.Orphan{{ID: "session:1", RootPID: proc.PID, StartTime: proc.StartTime, Verified: true, PIDs: pids}})
			if !errors.Is(err, outputErr) || len(report.Outcomes) != 1 {
				t.Fatalf("report=%+v err=%v", report, err)
			}
			outcome := report.Outcomes[0]
			if outcome.Status != want {
				t.Errorf("status=%s want %s", outcome.Status, want)
			}
			if early {
				if confirmed || p.calls != 0 || p.aliveCalls != 0 || outcome.Result != nil {
					t.Fatalf("early failure reached action: %+v platform=%+v", outcome, p)
				}
			} else if !confirmed || outcome.Result == nil || want != "refused" && !p.observed {
				t.Errorf("completed action lost: report=%+v platform=%+v", report, p)
			}
			if want == "completed" && (len(outcome.Result.Stopped) != 1 || report.Completed != 1 || outcome.Error.Code != "output_failed") {
				t.Errorf("completed action lost: %+v", report)
			}
			if want == "unknown" && (!errors.Is(err, context.Canceled) || outcome.Error.Code != "kill_failed") {
				t.Errorf("action error lost: %+v %v", outcome, err)
			}
			history, readErr := reclaim.Read(dir, 10, "process")
			if readErr != nil || len(history.Entries) != 1 {
				t.Fatalf("history=%+v err=%v", history, readErr)
			}
			if history.Entries[0].Status != want {
				t.Errorf("receipt status=%s want %s", history.Entries[0].Status, want)
			}
		})
	}
}

func TestReceiptKeepsPartialAccountingWithoutSensitiveText(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DRIGA_DATA_DIR", dir)
	outcome := KillOutcome{ID: "tree-1", MachineID: "host-1", Status: "partial", Reason: "SECRET_REASON", Error: &view.ErrorRecord{Code: "kill_failed", Message: "SECRET_COMMAND"}, Result: &kill.Result{Signaled: []int32{42, 43}, Stopped: []int32{42}, Survivors: []int32{43}, Unknown: []int32{44}, Reason: "SECRET_PATH", Before: derive.MemoryUsage{Processes: 3, Memory: 300, Measure: "rss", RSS: 300}, StoppedUsage: derive.MemoryUsage{Processes: 1, Memory: 100, Measure: "rss", RSS: 100}}}
	if err := recordKillOutcome(time.Now(), outcome); err != nil {
		t.Fatal(err)
	}
	h, err := reclaim.Read(dir, 10, "process")
	if err != nil || len(h.Entries) != 1 {
		t.Fatalf("%+v %v", h, err)
	}
	e := h.Entries[0]
	if len(e.Signaled) != 2 || len(e.Stopped) != 1 || len(e.Survivors) != 1 || len(e.Unknown) != 1 || e.Before.Memory != 300 || e.StoppedUsage.Memory != 100 || e.ErrorCode != "kill_failed" {
		t.Fatalf("%+v", e)
	}
	b, err := os.ReadFile(filepath.Join(dir, "reclaim.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "SECRET") {
		t.Fatal("sensitive text entered durable receipt")
	}
}

func TestHistoryFailurePreservesOutcome(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(dir, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DRIGA_DATA_DIR", dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, _ := (KillOptions{}).Execute(ctx, Environment{Stdout: io.Discard}, config.Config{}, nil, []model.Orphan{{ID: "tree-1"}})
	if len(report.Outcomes) != 1 || report.Outcomes[0].Status != "cancelled" || len(report.Warnings) != 1 {
		t.Fatalf("%+v", report)
	}
}

func TestKillRefusesIdentityChangedDuringConsent(t *testing.T) {
	for _, change := range []string{"executable", "arguments", "owner", "partial", "display incomplete"} {
		t.Run(change, func(t *testing.T) {
			t.Setenv("DRIGA_DATA_DIR", t.TempDir())
			proc := model.Proc{PID: 123456789, PPID: 1, StartTime: time.Now().Add(-time.Hour), OwnerKnown: true, OwnerUID: uint32(os.Getuid()), Exe: "/fixture/helper", Cmdline: "helper secret", Argv: []string{"helper", "secret"}}
			if runtime.GOOS == "windows" {
				u, err := user.Current()
				if err != nil {
					t.Fatal(err)
				}
				proc.OwnerSID = u.Uid
			}
			if change == "display incomplete" {
				proc.Partial = true
			}
			p := &outputFailurePlatform{procs: []model.Proc{proc}}
			confirmed := false
			env := Environment{Stdout: io.Discard, Platform: func() platform.Platform { return p }, VerifyWindow: time.Nanosecond, Confirm: func(string) error {
				confirmed = true
				switch change {
				case "executable":
					p.procs[0].Exe = "/fixture/workload"
				case "arguments":
					p.procs[0].Argv[1] = "new-workload"
				case "owner":
					p.procs[0].OwnerKnown = false
				case "partial":
					p.procs[0].Partial = true
				}
				return nil
			}}
			report, err := (KillOptions{}).Execute(context.Background(), env, config.Config{}, p.procs, []model.Orphan{{ID: "session:1", RootPID: proc.PID, StartTime: proc.StartTime, Verified: true, PIDs: []int32{proc.PID}}})
			if err == nil || len(report.Outcomes) != 1 || report.Outcomes[0].Status != "refused" || report.Outcomes[0].Result != nil || p.aliveCalls != 0 {
				t.Fatalf("changed identity reached shutdown: report=%+v err=%v platform=%+v", report, err, p)
			}
			if change == "display incomplete" && (confirmed || p.calls != 0) {
				t.Fatal("incomplete display identity reached consent or live verification")
			}
		})
	}
}

func TestKillStoredIdentityRequiresCurrentMatchBeforeConsent(t *testing.T) {
	for _, scenario := range []string{"match", "changed image", "unknown owner", "partial"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("DRIGA_DATA_DIR", t.TempDir())
			proc := model.Proc{PID: 123456789, PPID: 1, StartTime: time.Now().Add(-time.Hour), OwnerKnown: true, OwnerUID: uint32(os.Getuid()), Exe: "/fixture/helper", Cmdline: "helper", Argv: []string{"helper"}}
			if runtime.GOOS == "windows" {
				u, err := user.Current()
				if err != nil {
					t.Fatal(err)
				}
				proc.OwnerSID = u.Uid
			}
			selected := proc
			// SQLite keeps the displayed image and args, but omits owner fields.
			selected.OwnerKnown, selected.OwnerUID, selected.OwnerSID = false, 0, ""
			switch scenario {
			case "changed image":
				proc.Exe = "/fixture/workload"
			case "unknown owner":
				proc.OwnerKnown = false
			case "partial":
				proc.Partial = true
			}
			p := &outputFailurePlatform{procs: []model.Proc{proc}}
			confirmed := false
			env := Environment{Stdout: io.Discard, Platform: func() platform.Platform { return p }, Confirm: func(string) error { confirmed = true; return ErrRefused }}
			report, err := (KillOptions{}).Execute(context.Background(), env, config.Config{}, []model.Proc{selected}, []model.Orphan{{ID: "session:1", RootPID: proc.PID, StartTime: proc.StartTime, Verified: true}})
			if err == nil || confirmed != (scenario == "match") || p.aliveCalls != 0 || len(report.Outcomes) != 1 || report.Outcomes[0].Result != nil {
				t.Fatalf("stored selection adopted changed identity: %+v err=%v confirmed=%t", report, err, confirmed)
			}
		})
	}
}
