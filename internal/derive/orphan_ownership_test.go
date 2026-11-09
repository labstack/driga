package derive

import (
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

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
