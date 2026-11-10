package derive

import (
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestOrphanDirectoryOverlapNeedsOwnershipReview(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, sourceOS, helper, session, label string
		wantReview                             bool
	}{
		{"same", "linux", "/repo", "/repo", "working", true},
		{"helper below session", "linux", "/repo/pkg", "/repo", "working", true},
		{"session below helper", "linux", "/repo", "/repo/pkg", "working", true},
		{"siblings", "linux", "/repo/pkg", "/repo/other", "working", false},
		{"prefix is not containment", "linux", "/repo-other", "/repo", "working", false},
		{"ended session", "linux", "/repo/pkg", "/repo", "ended", false},
		{"dead session", "linux", "/repo/pkg", "/repo", "dead", false},
		{"windows case and separators", "windows", `C:\Repo\pkg`, "c:/repo", "working", true},
		{"windows parent", "windows", `C:\Repo`, "c:/repo/pkg", "working", true},
		{"windows different volume", "windows", `D:\Repo\pkg`, "c:/repo", "working", false},
		{"windows UNC", "windows", `\\server\share\repo\pkg`, "//SERVER/share/repo", "working", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx, err := NewProcIndexForOS([]model.Proc{
				{PID: 10, PPID: 1, StartTime: start, Cwd: tc.helper},
				{PID: 90, StartTime: start, Cwd: tc.session},
			}, tc.sourceOS)
			if err != nil {
				t.Fatal(err)
			}
			sessions := []model.Session{{ID: 1, Label: tc.label, SessionObs: model.SessionObs{PID: 90, ProcStart: start, Cwd: tc.session}}}
			reason := OrphanOwnershipReason(idx, sessions, 10)
			if got := strings.Contains(reason, "ownership requires review"); got != tc.wantReview {
				t.Fatalf("ownership review = %t, want %t: %q", got, tc.wantReview, reason)
			}
		})
	}
}

func TestOrphanSameDirectoryNeedsOwnershipReview(t *testing.T) {
	procs, sigs, now := orphanFixture(t)
	procs = append(procs, model.Proc{PID: 90, StartTime: now.Add(-time.Hour), Cwd: "/repo"})
	sessions := []model.Session{{ID: 1, Label: "working", SessionObs: model.SessionObs{PID: 90, ProcStart: now.Add(-time.Hour), Cwd: "/repo"}}}
	opts := WithExternalPeers(map[int32]model.PeerState{10: model.PeerNone, 11: model.PeerNone, 12: model.PeerNone})
	first := FindOrphans(unixOrphanIndex(procs), sessions, sigs, now, 0, nil, opts)
	if len(first) != 1 {
		t.Fatalf("review candidate missing: %+v", first)
	}
	second := FindOrphans(unixOrphanIndex(procs), sessions, sigs, now.Add(time.Minute), 0, map[string]model.Orphan{first[0].ID: first[0]}, opts)
	if len(second) != 1 || second[0].Verified || !strings.Contains(strings.Join(second[0].Evidence, ";"), "ownership requires review") {
		t.Fatalf("idle is not proof of disconnection: %+v", second)
	}
}

func TestOrphanRememberedLiveOwnerProtectedAfterReparenting(t *testing.T) {
	procs, sigs, now := orphanFixture(t)
	procs = append(procs, model.Proc{PID: 90, StartTime: now.Add(-time.Hour), Cwd: "/elsewhere"})
	sessions := []model.Session{{ID: 1, Label: "working", SessionObs: model.SessionObs{PID: 90, ProcStart: now.Add(-time.Hour), Cwd: "/elsewhere"}}}
	first := FindOrphans(unixOrphanIndex(procs), sessions, sigs, now, 0, nil)
	if len(first) != 1 {
		t.Fatal(first)
	}
	got := FindOrphans(unixOrphanIndex(procs), sessions, sigs, now, 0, nil, WithSpawnMemory(map[string]int64{first[0].ID: 1}))
	if len(got) != 0 {
		t.Fatalf("live spawning owner must protect reparented helper: %+v", got)
	}
}

func TestOrphanUnknownLiveSessionDirectoryNeedsReview(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	idx := NewProcIndex([]model.Proc{{PID: 10, PPID: 1, StartTime: start, Cwd: "/repo"}, {PID: 90, StartTime: start}})
	for _, hosted := range []bool{false, true} {
		pid := int32(90)
		if hosted {
			pid = 0
		}
		session := model.Session{ID: 1, Label: "working", SessionObs: model.SessionObs{PID: pid, ProcStart: start, Hosted: hosted}}
		if reason := OrphanOwnershipReason(idx, []model.Session{session}, 10); !strings.Contains(reason, "ownership requires review") {
			t.Fatalf("unknown live directory authorized ownership: %q", reason)
		}
		session.Label = "ended"
		if reason := OrphanOwnershipReason(idx, []model.Session{session}, 10); reason != "" {
			t.Fatalf("ended session must not protect unrelated work: %q", reason)
		}
	}
}
