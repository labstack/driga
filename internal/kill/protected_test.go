package kill

import (
	"os"
	"os/user"
	"runtime"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

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
// parent PID that a newer, unrelated process now has. That process is neither
// Driga's ancestor nor its descendant.
func TestProtectedReasonIgnoresReusedParentPID(t *testing.T) {
	now := time.Now()
	driga := ownedProc(t, 500, 732, "/opt/driga", now.Add(-time.Hour))
	target := ownedProc(t, 732, 900, "/bin/sleep", now)
	live := []model.Proc{driga, target, ownedProc(t, 900, 1, "/bin/sh", now.Add(-2*time.Hour))}
	if reason := ProtectedReason(target, live, nil); reason != "" {
		t.Fatalf("process with a reused PID protected: %s", reason)
	}
	// A real parent, started before Driga, stays protected.
	target.StartTime = now.Add(-2 * time.Hour)
	live[1] = target
	if reason := ProtectedReason(target, live, nil); reason == "" {
		t.Fatal("Driga's parent not protected")
	}
	// A child Driga started after itself stays protected; a stale child link
	// from a process older than its claimed parent does not.
	child := ownedProc(t, 800, 500, "/bin/sleep", now)
	if reason := ProtectedReason(child, []model.Proc{driga, child}, nil); reason == "" {
		t.Fatal("Driga's child not protected")
	}
	child.StartTime = now.Add(-2 * time.Hour)
	if reason := ProtectedReason(child, []model.Proc{driga, child}, nil); reason != "" {
		t.Fatalf("older process with a reused parent PID protected: %s", reason)
	}
}
