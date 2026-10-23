package derive

import (
	"reflect"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func familySession(id int64, pid int32) model.Session {
	return model.Session{ID: id, SessionObs: model.SessionObs{PID: pid, Cwd: "/repo", StartedAt: time.Unix(100, 0)}}
}

func TestLinkParentsByAncestry(t *testing.T) {
	idx := NewProcIndex([]model.Proc{{PID: 100, PPID: 1}, {PID: 101, PPID: 100}, {PID: 102, PPID: 101}, {PID: 103, PPID: 102}})
	sessions := []model.Session{familySession(1, 100), familySession(2, 102), familySession(3, 103)}
	if got := LinkParents(sessions, idx); !reflect.DeepEqual(got, map[int64]int64{2: 1, 3: 2}) {
		t.Fatal(got)
	}
}

func TestLinkParentsBrokerFallback(t *testing.T) {
	base := []model.Proc{{PID: 100, PPID: 1}, {PID: 101, PPID: 100, Cmdline: "node /plugin/app-server-broker.mjs", Argv: []string{"node", "/plugin/app-server-broker.mjs"}, Cwd: "/repo", StartTime: time.Unix(200, 0)}}
	sessions := []model.Session{familySession(1, 100), familySession(2, 0)}
	sessions[1].StartedAt = time.Unix(230, 0)
	if got := LinkParents(sessions, NewProcIndex(base)); !reflect.DeepEqual(got, map[int64]int64{2: 1}) {
		t.Fatal(got)
	}
	for _, name := range []string{"before lifetime", "wrong cwd", "not helper"} {
		t.Run(name, func(t *testing.T) {
			procs := append([]model.Proc(nil), base...)
			switch name {
			case "before lifetime":
				procs[1].StartTime = time.Unix(99, 0)
			case "wrong cwd":
				procs[1].Cwd = "/other"
			case "not helper":
				procs[1].Cmdline = "shell"
				procs[1].Argv = []string{"shell"}
			}
			if got := LinkParents(sessions, NewProcIndex(procs)); len(got) != 0 {
				t.Fatal(got)
			}
		})
	}
}

func TestLinkParentsHostedNeedsBrokerNearThreadStart(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, detached := range []bool{false, true} {
		for _, tc := range []struct {
			name string
			age  time.Duration
			link bool
		}{
			{"two hours before", 2 * time.Hour, false},
			{"thirty seconds before", 30 * time.Second, true},
			{"two minute boundary", 2 * time.Minute, true},
			{"outside window", 2*time.Minute + time.Nanosecond, false},
			{"after thread", -time.Second, false},
		} {
			name := "ancestry/"
			if detached {
				name = "detached/"
			}
			t.Run(name+tc.name, func(t *testing.T) {
				parent := familySession(1, 100)
				parent.StartedAt = now.Add(-3 * time.Hour)
				child := familySession(2, 0)
				child.Hosted, child.StartedAt = true, now
				ppid := int32(100)
				if detached {
					ppid = 1
				}
				procs := []model.Proc{
					{PID: 100, PPID: 1, Cwd: "/repo", StartTime: parent.StartedAt},
					{PID: 200, PPID: ppid, Cwd: "/repo", StartTime: now.Add(-tc.age), Role: "helper"},
				}
				got := LinkParents([]model.Session{parent, child}, NewProcIndex(procs))
				if tc.link && got[2] != 1 || !tc.link && len(got) != 0 {
					t.Fatalf("links=%v, want linked=%v", got, tc.link)
				}
				if !tc.link {
					child.Completions = []model.CompletionEvent{{ID: "turn", TS: now}}
					items := InboxItems([]model.Session{parent, child}, got, nil, time.Time{})
					if len(items) != 1 || items[0].Kind != "finished" || items[0].SessionID != child.ID {
						t.Fatalf("unlinked completion must reach inbox: %+v", items)
					}
				}
			})
		}
	}
	t.Run("nearest broker wins", func(t *testing.T) {
		first, second := familySession(1, 100), familySession(2, 101)
		first.StartedAt, second.StartedAt = now.Add(-time.Hour), now.Add(-time.Hour)
		child := familySession(3, 0)
		child.Hosted, child.StartedAt = true, now
		procs := []model.Proc{
			{PID: 100, PPID: 1}, {PID: 101, PPID: 1}, {PID: 150, PPID: 101},
			{PID: 200, PPID: 100, Cwd: "/repo", StartTime: now.Add(-time.Minute), Role: "helper"},
			{PID: 201, PPID: 150, Cwd: "/repo", StartTime: now.Add(-10 * time.Second), Role: "host"},
		}
		if got := LinkParents([]model.Session{child, second, first}, NewProcIndex(procs)); !reflect.DeepEqual(got, map[int64]int64{3: 2}) {
			t.Fatalf("nearest broker must win over shallower or lower-ID broker: %v", got)
		}
	})
	t.Run("missing thread start", func(t *testing.T) {
		parent, child := familySession(1, 100), familySession(2, 0)
		child.StartedAt = time.Time{}
		procs := []model.Proc{{PID: 100, PPID: 1}, {PID: 200, PPID: 100, Cwd: "/repo", Role: "helper", StartTime: now}}
		if got := LinkParents([]model.Session{parent, child}, NewProcIndex(procs)); len(got) != 0 {
			t.Fatalf("no evidence for broker/thread timing: %v", got)
		}
	})
}

func TestProcIndexCyclesAndOrder(t *testing.T) {
	idx := NewProcIndex([]model.Proc{{PID: 3, PPID: 2}, {PID: 2, PPID: 1}, {PID: 1, PPID: 3}, {PID: 4, PPID: 2}})
	if got := idx.Ancestors(1); !reflect.DeepEqual(got, []int32{3, 2}) {
		t.Fatal(got)
	}
	var pids []int32
	for _, p := range idx.Descendants(1) {
		pids = append(pids, p.PID)
	}
	if !reflect.DeepEqual(pids, []int32{2, 3, 4}) {
		t.Fatal(pids)
	}
	if p, ok := idx.Get(4); !ok || p.PPID != 2 {
		t.Fatal(p, ok)
	}
	if _, ok := idx.Get(99); ok {
		t.Fatal("unknown pid found")
	}
	links := LinkParents([]model.Session{familySession(1, 1), familySession(2, 2), familySession(3, 3)}, idx)
	for id := range links {
		seen := map[int64]bool{}
		for cur := id; cur != 0; cur = links[cur] {
			if seen[cur] {
				t.Fatalf("cycle: %v", links)
			}
			seen[cur] = true
		}
	}
}
