package derive

import (
	"reflect"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestSameStart(t *testing.T) {
	a := time.Unix(1700000000, 0)
	if !SameStart(a, a.Add(900*time.Millisecond)) || !SameStart(a.Add(900*time.Millisecond), a) {
		t.Fatal("sub-second skew must match")
	}
	if SameStart(a.Add(2*time.Second), a) {
		t.Fatal("2s apart is a different process")
	}
}

func TestSameStartAsymmetric(t *testing.T) {
	create := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		name             string
		create, recorded time.Time
		want             bool
	}{
		{"registry delayed", create, create.Add(30 * time.Second), true},
		{"new process", create.Add(2 * time.Second), create, false},
		{"late boundary", create, create.Add(60 * time.Second), true},
		{"too late", create, create.Add(60*time.Second + time.Nanosecond), false},
		{"early boundary", create.Add(time.Second), create, true},
		{"too early", create.Add(time.Second + time.Nanosecond), create, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := SameStart(tc.create, tc.recorded); got != tc.want {
				t.Fatalf("SameStart(%s, %s) = %v; want %v", tc.create, tc.recorded, got, tc.want)
			}
		})
	}
}

// Claude's registry startedAt and the kernel create time differ by milliseconds.
func TestLinkParentsToleratesRegistryStartSkew(t *testing.T) {
	start := time.Unix(1700000000, 0)
	idx := NewProcIndex([]model.Proc{{PID: 100, PPID: 1, StartTime: start}, {PID: 101, PPID: 100, StartTime: start.Add(time.Minute)}})
	parent := familySession(1, 100)
	parent.ProcStart = start.Add(-250 * time.Millisecond)
	child := familySession(2, 101)
	if got := LinkParents([]model.Session{parent, child}, idx); !reflect.DeepEqual(got, map[int64]int64{2: 1}) {
		t.Fatal(got)
	}
}

// A detached broker (PPID 1) has no session in its ancestry; spec §7.1a falls
// back to broker cwd + start time inside a live session's lifetime.
func TestLinkParentsDetachedBrokerByCwdAndLifetime(t *testing.T) {
	start := time.Unix(1000, 0)
	procs := []model.Proc{
		{PID: 100, PPID: 1, StartTime: start, Cwd: "/repo"},
		{PID: 200, PPID: 1, StartTime: start.Add(time.Hour), Cwd: "/repo", Cmdline: "node /plugin/app-server-broker.mjs serve", Argv: []string{"node", "/plugin/app-server-broker.mjs", "serve"}},
	}
	parent := familySession(1, 100)
	parent.Cwd, parent.StartedAt = "/repo", start
	hosted := familySession(2, 0)
	hosted.Cwd, hosted.Hosted = "/repo", true
	hosted.StartedAt = procs[1].StartTime.Add(30 * time.Second)
	if got := LinkParents([]model.Session{parent, hosted}, NewProcIndex(procs)); !reflect.DeepEqual(got, map[int64]int64{2: 1}) {
		t.Fatalf("want hosted linked to session in same cwd, got %v", got)
	}
	// Broker started before the session existed: not its child.
	parent.StartedAt = start.Add(2 * time.Hour)
	if got := LinkParents([]model.Session{parent, hosted}, NewProcIndex(procs)); len(got) != 0 {
		t.Fatalf("broker older than session must not link, got %v", got)
	}
}
