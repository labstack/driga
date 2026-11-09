package reclaim

import (
	"testing"
	"time"
)

func TestMergeReviewCannotWeakenProtectionOrAuthorizeGenericStop(t *testing.T) {
	at := time.Now().Add(-time.Hour)
	for _, status := range []string{"protected", "ready"} {
		old := []Opportunity{{ID: "orphan", RootPID: 10, StartedAt: at, Status: status, Action: "driga orphans kill orphan"}}
		review := ProcessReview{RootPID: 10, StartTime: at, Status: "protected", Reasons: []string{"live owner"}}
		rows := MergeProcessReviews(old, []ProcessReview{review}, time.Now())
		if len(rows) != 1 || rows[0].Status != "protected" || rows[0].Action != "" || old[0].Status != status {
			t.Fatalf("unsafe merge or mutation: %+v", rows)
		}
	}
	rows := MergeProcessReviews(nil, []ProcessReview{{RootPID: 20, ID: "process:20", StartTime: at, Cwd: "/work/project", Status: "review"}}, time.Now())
	if len(rows) != 1 || rows[0].Status == "ready" || rows[0].Action != "driga reclaim --pid 20" || rows[0].Cwd != "/work/project" {
		t.Fatalf("generic stop authorization: %+v", rows)
	}
}

func TestMergeReviewInvalidatesUnmatchedReadyIdentity(t *testing.T) {
	at := time.Now().Add(-time.Hour)
	for _, reviews := range [][]ProcessReview{nil, {{ID: "new-image", RootPID: 10, StartTime: at.Add(time.Second), Status: "review"}}} {
		old := []Opportunity{{ID: "old-image", Kind: "process", RootPID: 10, StartedAt: at, Status: "ready", ResourcesKnown: true, Action: "driga orphans kill old-image"}}
		rows := MergeProcessReviews(old, reviews, time.Now())
		for _, row := range rows {
			if row.ID == "old-image" && (row.Status == "ready" || row.Action != "" || row.ResourcesKnown) {
				t.Fatalf("stale identity remained actionable: %+v", row)
			}
		}
	}
}
