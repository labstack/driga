package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/flowtest"
	"github.com/labstack/driga/internal/view"
)

func TestFlowAttentionHTMLHasSessionHints(t *testing.T) {
	s, st, _, _ := fixture(t)
	if err := st.Tx(context.Background(), flowtest.SeedAmbiguousAttention); err != nil {
		t.Fatal(err)
	}
	for _, policy := range []string{"titles", "none"} {
		f, err := view.Flow(st, view.Filter{CaptureText: policy}, 0, flowtest.Now)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := view.LoadBoardSnapshot(st, view.Filter{CaptureText: policy}, flowtest.Now)
		if err != nil {
			t.Fatal(err)
		}
		var out strings.Builder
		if err := s.templates.ExecuteTemplate(&out, "flow", pageData{Board: view.Board(snapshot, time.Hour, flowtest.Now)}); err != nil {
			t.Fatal(err)
		}
		want := []string{}
		for _, r := range f.Attention.Waits {
			if r.Title != "" {
				want = append(want, r.Title)
			}
		}

		for _, hint := range want {
			parts := strings.Split(hint, " · ")
			if !strings.Contains(out.String(), parts[len(parts)-1]) {
				t.Errorf("%s missing %q: %s", policy, hint, out.String())
			}
		}
	}
}
