package view

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/flowtest"
)

func TestFlowAttentionDistinguishesSessionsAndHonorsCapture(t *testing.T) {
	for _, policy := range []string{"titles", "snippets", "none"} {
		t.Run(policy, func(t *testing.T) {
			st := fixture(t, flowtest.SeedAmbiguousAttention)
			f, err := Flow(st, Filter{CaptureText: policy}, 0, flowtest.Now)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"sample · Fix cache", "sample · Fix parser", "sample · #ccc333", "sample · #ddd444"}
			if policy == "none" {
				want[0], want[1] = "sample · work/cache", "sample · work/parser"
			}
			if len(f.Attention.Waits) != len(want) {
				t.Fatal(f.Attention)
			}
			for i, row := range f.Attention.Waits {
				if row.Title != want[i] {
					t.Errorf("card %d: %q, want %q", i, row.Title, want[i])
				}
			}
			if policy == "none" {
				raw, err := json.Marshal(f.Attention)
				if err != nil || strings.Contains(string(raw), "Fix ") {
					t.Fatalf("capture none exposed title: %s (%v)", raw, err)
				}
			}
		})
	}
}
