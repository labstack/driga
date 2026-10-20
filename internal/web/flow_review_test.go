package web

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

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
		var out strings.Builder
		if err := s.templates.ExecuteTemplate(&out, "flow", pageData{Flow: f}); err != nil {
			t.Fatal(err)
		}
		want := []string{"sample · Fix cache", "sample · Fix parser", "sample · #ccc333", "sample · #ddd444"}
		if policy == "none" {
			want[0], want[1] = "sample · work/cache", "sample · work/parser"
		}
		for _, hint := range want {
			if !strings.Contains(out.String(), hint) {
				t.Errorf("%s missing %q: %s", policy, hint, out.String())
			}
		}
	}
}

func TestFlowDetailJavaScriptUsesSharedDuration(t *testing.T) {
	runtime, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	js, err := assets.ReadFile("static/flow-map.js")
	if err != nil {
		t.Fatal(err)
	}
	harness, err := os.ReadFile("testdata/flow-review.js")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(runtime, "-e", string(harness)+"\n"+string(js))
	cmd.Env = []string{"PATH=" + t.TempDir(), "HOME=" + t.TempDir()}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("detail: %v: %s", err, out)
	}
}
