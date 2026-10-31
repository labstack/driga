package kill

import (
	"os"
	"os/user"
	"runtime"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

// PIDs far above any the test runner's own processes have.
const fakePID = 1 << 30

func ownedProc(t *testing.T, pid, ppid int32, exe string, start time.Time) model.Proc {
	t.Helper()
	p := model.Proc{PID: pid, PPID: ppid, Exe: exe, Cmdline: exe, Argv: []string{exe}, StartTime: start, OwnerKnown: true, OwnerUID: uint32(os.Getuid())}
	if runtime.GOOS == "windows" {
		u, err := user.Current()
		if err != nil {
			t.Fatal(err)
		}
		p.OwnerUID, p.OwnerSID = 0, u.Uid
	}
	return p
}

// Windows keeps the PID of an exited parent, so a Driga process can name a
// parent PID that a newer, unrelated process now has. On Windows that process
// is neither Driga's ancestor nor its descendant; elsewhere a PPID always
// names the live parent.
func TestProtectionReusedParentPID(t *testing.T) {
	now := time.Now()
	driga := ownedProc(t, fakePID+1, fakePID+2, "/opt/driga", now.Add(-time.Hour))
	newer := ownedProc(t, fakePID+2, fakePID+3, "/bin/sleep", now)
	shell := ownedProc(t, fakePID+3, 1, "/bin/sh", now.Add(-2*time.Hour))
	live := []model.Proc{driga, newer, shell}
	if reason := newProtection(live, nil, true).Reason(newer); reason != "" {
		t.Fatalf("Windows: process with a reused PID protected: %s", reason)
	}
	if newProtection(live, nil, true).Reason(shell) != "" {
		t.Fatal("Windows: the reused PID's parent protected")
	}
	if newProtection(live, nil, false).Reason(newer) == "" {
		t.Fatal("Unix: Driga's parent not protected")
	}
	// A real parent, started before Driga, and Driga's children stay
	// protected everywhere, as does a child older than its root.
	older := ownedProc(t, fakePID+2, fakePID+3, "/bin/sleep", now.Add(-2*time.Hour))
	child := ownedProc(t, fakePID+4, fakePID+1, "/bin/sleep", now.Add(-3*time.Hour))
	for _, windows := range []bool{false, true} {
		pr := newProtection([]model.Proc{driga, older, shell, child}, nil, windows)
		if pr.Reason(older) == "" || pr.Reason(shell) == "" || pr.Reason(child) == "" {
			t.Fatalf("windows=%v: Driga's family not protected", windows)
		}
	}
}

// A broker root protects its children even after it exited, and stops
// protecting a PID only when a clearly newer process holds it.
func TestProtectionBrokerRoots(t *testing.T) {
	now := time.Now()
	root := ownedProc(t, fakePID+5, 1, "/bin/agent", now.Add(-time.Hour))
	child := ownedProc(t, fakePID+6, fakePID+5, "/bin/sleep", now)
	for _, windows := range []bool{false, true} {
		if newProtection([]model.Proc{child}, []model.Proc{root}, windows).Reason(child) == "" {
			t.Fatalf("windows=%v: exited root's child not protected", windows)
		}
	}
	rounded := root
	rounded.StartTime = root.StartTime.Add(500 * time.Millisecond)
	if newProtection([]model.Proc{rounded}, []model.Proc{root}, true).Reason(rounded) == "" {
		t.Fatal("root with a slightly different start time not protected")
	}
	reused := ownedProc(t, fakePID+5, 1, "/bin/sleep", now)
	if reason := newProtection([]model.Proc{reused}, []model.Proc{root}, true).Reason(reused); reason != "" {
		t.Fatalf("newer process with a root's PID protected: %s", reason)
	}
}
