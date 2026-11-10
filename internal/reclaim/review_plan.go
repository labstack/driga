package reclaim

import (
	"fmt"
	"strings"
	"time"

	"github.com/labstack/driga/internal/derive"
)

// MergeProcessReviews adds native observations without weakening an existing
// protection or treating general review as permission to stop a process.
func MergeProcessReviews(rows []Opportunity, reviews []ProcessReview, at time.Time) []Opportunity {
	rows = append([]Opportunity{}, rows...)
	for i, row := range rows {
		if row.Kind != "process" {
			continue
		}
		matched := false
		for _, review := range reviews {
			if row.RootPID == review.RootPID && !row.StartedAt.IsZero() && row.StartedAt.Equal(review.StartTime) {
				matched = true
				break
			}
		}
		if !matched {
			if rows[i].Status == "ready" {
				rows[i].Status = "review"
			}
			rows[i].Action, rows[i].ResourcesKnown = "", false
			rows[i].CPU, rows[i].Memory = 0, derive.MemoryUsage{}
			rows[i].Reason += "; process identity or eligibility was not established in the current native review; inspect the exact PID"
		}
	}
	for _, review := range reviews {
		row := Opportunity{ID: review.ID, Kind: "process_review", RootPID: review.RootPID, PPID: review.PPID,
			StartedAt: review.StartTime, ObservedAt: at, Name: review.Name, Cwd: review.Cwd, Category: review.Category,
			AgeMS: review.Age.Milliseconds(), PIDs: review.PIDs, Status: review.Status, Reason: strings.Join(review.Reasons, "; "),
			Memory: review.Memory, ResourcesKnown: true, CPU: review.CPU, CPUKnown: &review.CPUKnown,
			IdentityKnown: &review.IdentityKnown, OwnershipKnown: &review.OwnershipKnown, PeersKnown: &review.PeersKnown,
			Action: fmt.Sprintf("driga reclaim --pid %d", review.RootPID)}
		merged := false
		for i := range rows {
			old := rows[i]
			if old.RootPID != row.RootPID || old.StartedAt.IsZero() || !old.StartedAt.Equal(row.StartedAt) {
				continue
			}
			row.ID, row.Kind, row.Action = old.ID, old.Kind, old.Action
			row.Reason = old.Reason + "; " + row.Reason
			if old.Status == "protected" || row.Status == "protected" {
				row.Status, row.Action = "protected", ""
			} else if old.Status == "ready" && review.IdentityKnown && review.OwnershipKnown && review.PeersKnown && review.CPUKnown {
				row.Status = "ready"
			}
			rows[i], merged = row, true
			break
		}
		if !merged {
			rows = append(rows, row)
		}
	}
	SortOpportunities(rows)
	return rows
}
