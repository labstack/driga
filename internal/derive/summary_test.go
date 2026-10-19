package derive

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

// Handoffs follow the titles' 24-hour window and keep only the newest few, so
// a long-lived project cannot grow the summary prompt without bound.
func TestSummaryInputBoundsHandoffs(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	var hs []model.CompletionEvent
	for i := range 8 {
		hs = append(hs, model.CompletionEvent{TS: now.Add(-time.Duration(i) * time.Hour), Text: fmt.Sprintf("recent %d", i)})
	}
	hs = append(hs, model.CompletionEvent{TS: now.Add(-25 * time.Hour), Text: "old"})
	in := BuildSummaryInput(model.Repo{Name: "p"}, nil, nil, hs, now, true, nil)
	want := []string{"recent 0", "recent 1", "recent 2", "recent 3", "recent 4"}
	if !slices.Equal(in.Handoffs, want) {
		t.Fatalf("handoffs: %q", in.Handoffs)
	}
}
