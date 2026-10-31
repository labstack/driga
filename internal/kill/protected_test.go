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
	reason := NewProtection([]model.Proc{driga, newer, shell}, nil).Reason(newer)
	if runtime.GOOS == "windows" && reason != "" {
		t.Fatalf("process with a reused PID protected: %s", reason)
	}
	if runtime.GOOS != "windows" && reason == "" {
		t.Fatal("Driga's parent not protected")
	}
	// A real parent, started before Driga, stays protected everywhere.
	older := ownedProc(t, fakePID+2, fakePID+3, "/bin/sleep", now.Add(-2*time.Hour))
	if NewProtection([]model.Proc{driga, older, shell}, nil).Reason(older) == "" {
		t.Fatal("Driga's parent not protected")
	}
	child := ownedProc(t, fakePID+4, fakePID+1, "/bin/sleep", now)
	if NewProtection([]model.Proc{driga, child}, nil).Reason(child) == "" {
		t.Fatal("Driga's child not protected")
	}
}

// A broker root that exited and whose PID a newer process reused no longer
// protects that process.
func TestProtectionExitedBrokerRoot(t *testing.T) {
	now := time.Now()
	reused := ownedProc(t, fakePID+5, 1, "/bin/sleep", now)
	exited := reused
	exited.StartTime = now.Add(-time.Hour)
	if reason := NewProtection([]model.Proc{reused}, []model.Proc{exited}).Reason(reused); reason != "" {
		t.Fatalf("reused root PID protected: %s", reason)
	}
	if NewProtection([]model.Proc{reused}, []model.Proc{reused}).Reason(reused) == "" {
		t.Fatal("live broker root not protected")
	}
}
