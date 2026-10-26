package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/panel"
)

func TestLiveCommittedOutcomes(t *testing.T) {
	for _, failure := range []string{"source", "encoding"} {
		t.Run(failure, func(t *testing.T) {
			store := New(t.TempDir())
			calls := 0
			live := NewLive(store, func(_ context.Context, req panel.RunRequest) (panel.Fragment, error) {
				calls++
				if failure == "source" {
					return panel.Fragment{}, errors.New("snapshot unavailable")
				}
				return panel.Fragment{Dashboard: req.Dashboard, Results: []panel.Result{{ID: "w", Frame: &panel.Frame{Rows: [][]any{{math.NaN()}}}}}}, nil
			})
			ctx := context.Background()
			spec := panel.Dashboard{Name: "Committed", Panels: []panel.Panel{{ID: "w", Title: "W", Source: "inbox", Viz: "stat"}}}
			f, err := live.Create(ctx, spec, Author{Kind: "user"}, "", nil)
			id := f.ID
			check := func(version int) {
				t.Helper()
				if err != nil || f.ID != id || f.Version != version || len(f.Results) != 1 || !strings.Contains(f.Results[0].Message, "live data unavailable") {
					t.Fatalf("outcome: %+v %v", f, err)
				}
				if _, err := json.Marshal(f); err != nil {
					t.Fatal(err)
				}
				r, err := store.Get(id)
				if err != nil || r.Version != version || r.Name != f.Dashboard.Name {
					t.Fatalf("commit: %+v %v", r, err)
				}
			}
			check(1)
			f, err = live.Edit(ctx, id, []Operation{{Op: "rename", Name: "Edited"}}, 1, Author{}, "", nil)
			check(2)
			f, err = live.Replace(ctx, id, spec, 2, Author{}, "", nil)
			check(3)
			f, err = live.Restore(ctx, id, 1, 3, Author{}, nil)
			check(4)
			if _, err := live.Edit(ctx, id, nil, 1, Author{}, "", nil); !errors.Is(err, ErrStale) {
				t.Fatalf("stale save: %v", err)
			}
			if calls != 4 {
				t.Fatalf("live calls = %d; saves were retried or stale edit ran", calls)
			}
			f, err = live.Open(ctx, id, nil)
			check(4)
		})
	}
}
