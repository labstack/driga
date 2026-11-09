package reclaim

import (
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

func TestPlanProtectsAmbiguityAndAttributesNestedWorkOnce(t *testing.T) {
	at := time.Now().Add(-time.Hour)
	idx := derive.NewProcIndex([]model.Proc{{PID: 10, PPID: 1, StartTime: at, RSS: 100, CPUPercent: 25}, {PID: 11, PPID: 10, StartTime: at, RSS: 200, CPUPercent: 75}})
	sessions := []model.Session{{ID: 2, SessionObs: model.SessionObs{PID: 11, ProcStart: at}}, {ID: 1, SessionObs: model.SessionObs{PID: 10, ProcStart: at}}}
	orphans := []view.OrphanRow{{Orphan: model.Orphan{ID: "ambiguous", RootPID: 20, Verified: true, Evidence: []string{"ownership requires review: live session may need this tree"}}, ResourcesKnown: true, Memory: 5000}}
	disk := []cleanup.Candidate{{ID: "ready-cache", Status: "ready", Kind: cleanup.Cache, Size: 1000, SizeKnown: true}, {ID: "unknown-cache", Status: "review", Size: 9999, SizeKnown: false}}
	rows := Opportunities(disk, orphans, sessions, idx)
	if len(rows) != 4 || rows[0].ID != "ready-cache" {
		t.Fatalf("plan=%+v", rows)
	}
	for _, r := range rows {
		switch r.ID {
		case "ambiguous":
			if r.Status != "protected" || r.Action != "" {
				t.Fatalf("unsafe action: %+v", r)
			}
		case "session:1":
			if r.Status != "protected" || r.Name == "" || r.CPU != 100 || r.Memory.RSS != 300 || !strings.Contains(r.Action, "jump 1") {
				t.Fatalf("active work=%+v", r)
			}
		case "unknown-cache":
			if r.DiskKnown || r.Action != "" {
				t.Fatalf("invented disk savings/action: %+v", r)
			}
		}
	}
}

func TestPlanDoesNotTreatUnknownOrYoungProcessesAsReady(t *testing.T) {
	for _, o := range []view.OrphanRow{
		{Orphan: model.Orphan{ID: "unknown", Verified: true}},
		{Orphan: model.Orphan{ID: "young", Verified: true, Young: true}, ResourcesKnown: true},
	} {
		rows := Opportunities(nil, []view.OrphanRow{o}, nil, derive.NewProcIndex(nil))
		if rows[0].Status == "ready" || rows[0].Action != "" {
			t.Fatalf("unsafe plan %+v", rows)
		}
	}
}

func TestPlanOffersFreshVerificationForSnapshotCandidate(t *testing.T) {
	at := time.Now().Add(-time.Hour)
	idx := derive.NewProcIndex([]model.Proc{{PID: 10, PPID: 1, StartTime: at, RSS: 100}})
	rows := Opportunities(nil, []view.OrphanRow{{Orphan: model.Orphan{ID: "snapshot", RootPID: 10, StartTime: at}, ResourcesKnown: true}}, nil, idx)
	if rows[0].Status != "review" || rows[0].Action != "driga orphans kill snapshot" {
		t.Fatalf("snapshot cannot request verification: %+v", rows)
	}
}
