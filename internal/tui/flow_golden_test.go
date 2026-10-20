package tui

import (
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"context"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"github.com/labstack/driga/internal/flowtest"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFlowGoldenFrames(t *testing.T) {
	// Golden clocks stay deterministic; local-time conversion is covered separately.
	originalLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = originalLocal })
	now := flowtest.Now
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var id int64
	if err = st.Tx(context.Background(), func(tx *store.Tx) (e error) { id, e = flowtest.SeedMap(tx, now, true); return e }); err != nil {
		t.Fatal(err)
	}
	f, err := view.Flow(st, view.Filter{FlowMinutes: 240}, id, now)
	if err != nil {
		t.Fatal(err)
	}
	original := compat.HasDarkBackground
	compat.HasDarkBackground = true
	defer func() { compat.HasDarkBackground = original }()
	for _, width := range []int{80, 100, 140} {
		for _, color := range []bool{false, true} {
			name := fmt.Sprintf("flow-%d-color-%t.golden", width, color)
			t.Run(name, func(t *testing.T) {
				m := setup(t, fixture(), Options{Now: func() time.Time { return now }}).(*app)
				m.opts.NoColor = !color
				m.opts.MotionOff = true
				m.flow = f
				m.flowMinutes = 240
				m.width = width
				m.height = 60
				m.loaded = true
				m.tab = flowTab
				got := m.flowView() + "\n"
				for _, line := range strings.Split(got, "\n") {
					if lipgloss.Width(line) > width {
						t.Fatalf("overflow: %q", line)
					}
				}
				if !color && strings.Contains(got, "\x1b") {
					t.Fatal("NO_COLOR emitted ANSI")
				}
				path := filepath.Join("testdata", name)
				if os.Getenv("UPDATE_FLOW_GOLDEN") == "1" {
					t.Logf("regenerating %s (ANSI retained in colour golden):\n%s", name, ansi.Strip(got))
					if err := os.MkdirAll("testdata", 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(got), 0644); err != nil {
						t.Fatal(err)
					}
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if got != string(want) {
					t.Fatalf("golden drift: %s", path)
				}
			})
		}
	}
}
