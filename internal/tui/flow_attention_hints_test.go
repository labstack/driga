package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/flowtest"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

func TestFlowAttentionCardsHaveSessionHints(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Tx(context.Background(), flowtest.SeedAmbiguousAttention); err != nil {
		t.Fatal(err)
	}
	for _, policy := range []string{"titles", "none"} {
		f, err := view.Flow(st, view.Filter{CaptureText: policy}, 0, flowtest.Now)
		if err != nil {
			t.Fatal(err)
		}
		m := setup(t, fixture(), Options{Now: func() time.Time { return flowtest.Now }}).(*app)
		m.flow, m.width = f, 100
		got := m.flowView()
		want := []string{"sample · Fix cache", "sample · Fix parser", "sample · #ccc333", "sample · #ddd444"}
		if policy == "none" {
			want[0], want[1] = "sample · work/cache", "sample · work/parser"
		}
		for _, hint := range want {
			if !strings.Contains(got, hint) {
				t.Errorf("%s missing %q: %s", policy, hint, got)
			}
		}
	}
}
