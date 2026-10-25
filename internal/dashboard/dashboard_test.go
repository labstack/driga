package dashboard

import (
	"errors"
	"testing"

	"github.com/labstack/driga/internal/panel"
)

var you = Author{Kind: "user"}

func TestHomeExistsBeforeItIsSaved(t *testing.T) {
	s := New(t.TempDir())
	r, err := s.Get(panel.HomeID)
	if err != nil || r.Version != 0 || len(r.Spec.Panels) == 0 {
		t.Fatalf("home = %+v, %v", r, err)
	}
	edited, err := s.Edit(panel.HomeID, []Operation{{Op: "remove", ID: "orphans"}}, 0, you, "")
	if err != nil || edited.Version != 1 || len(edited.Spec.Panels) != len(r.Spec.Panels)-1 {
		t.Fatalf("edited home = %+v, %v", edited, err)
	}
	list, err := s.List()
	if err != nil || len(list) != 1 || list[0].ID != panel.HomeID || list[0].Version != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

func TestEditVersionsAndRestore(t *testing.T) {
	s := New(t.TempDir())
	spec := panel.Dashboard{Name: "Payments", Panels: []panel.Panel{{ID: "waits", Title: "Waits", Viz: "stat", Source: "inbox"}}}
	r, err := s.Create(spec, Author{Kind: "agent", Name: "claude"}, "first")
	if err != nil || r.Version != 1 {
		t.Fatalf("create = %+v, %v", r, err)
	}
	r, err = s.Edit(r.ID, []Operation{
		{Op: "add", Panel: &panel.Panel{ID: "mem", Title: "Memory", Viz: "stat", Source: "sessions", Measure: "sum:mem_bytes"}, Before: "waits"},
		{Op: "update", ID: "waits", Panel: &panel.Panel{Title: "Waiting on you"}},
		{Op: "layout", Grids: map[string]Grid{"mem": {X: 0, Y: 0, W: 6, H: 4}}},
	}, 1, you, "add memory")
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != 2 || r.Spec.Panels[0].ID != "mem" || r.Spec.Panels[1].Title != "Waiting on you" || r.Spec.Panels[1].Viz != "stat" || r.Spec.Panels[0].Grid.W != 6 {
		t.Fatalf("edited = %+v", r.Spec.Panels)
	}
	if _, err := s.Edit(r.ID, []Operation{{Op: "rename", Name: "x"}}, 1, you, ""); !errors.Is(err, ErrStale) {
		t.Fatalf("stale edit = %v", err)
	}
	var problems panel.Problems
	if _, err := s.Edit(r.ID, []Operation{{Op: "update", ID: "mem", Panel: &panel.Panel{Viz: "pie"}}}, 0, you, ""); !errors.As(err, &problems) {
		t.Fatalf("invalid edit = %v", err)
	}
	restored, err := s.Restore(r.ID, 1, you)
	if err != nil || restored.Version != 3 || len(restored.Spec.Panels) != 1 || restored.Spec.Panels[0].Title != "Waits" {
		t.Fatalf("restore = %+v, %v", restored, err)
	}
	versions, err := s.Versions(r.ID)
	if err != nil || len(versions) != 3 || versions[0].Version != 3 || versions[2].Author.Kind != "agent" {
		t.Fatalf("versions = %+v, %v", versions, err)
	}
	if _, err := s.Restore(r.ID, 3, you); !errors.Is(err, ErrAlreadyLatest) {
		t.Fatalf("restore current = %v", err)
	}
	if _, err := s.Get("../etc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("path id = %v", err)
	}
}

func TestReplaceKeepsDraggedLayout(t *testing.T) {
	s := New(t.TempDir())
	spec := panel.Dashboard{Name: "Board", Panels: []panel.Panel{{ID: "a", Title: "A", Viz: "stat", Source: "inbox"}}}
	r, _ := s.Create(spec, you, "")
	r, _ = s.Edit(r.ID, []Operation{{Op: "layout", Grids: map[string]Grid{"a": {X: 3, Y: 0, W: 3, H: 2}}}}, 0, you, "")
	r, err := s.Replace(r.ID, spec, r.Version, Author{Kind: "agent"}, "")
	if err != nil || r.Spec.Panels[0].Grid == nil || r.Spec.Panels[0].Grid.X != 3 {
		t.Fatalf("replace = %+v, %v", r.Spec.Panels, err)
	}
}
