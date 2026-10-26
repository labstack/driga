package dashboard

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
	"testing"

	"github.com/labstack/driga/internal/panel"
)

func TestCloneRejectsInvalidNumber(t *testing.T) {
	max := math.NaN()
	d := panel.Dashboard{Name: "Invalid", Panels: []panel.Panel{{ID: "memory", Title: "Memory", Viz: "stat", Source: "sessions", Max: &max}}}
	_, err := clone(d)
	var invalid *json.UnsupportedValueError
	if !errors.As(err, &invalid) {
		t.Fatalf("clone lost invalid-number error: %v", err)
	}
}

func TestCloneFailureDoesNotSaveRevision(t *testing.T) {
	s := New(t.TempDir())
	r, err := s.Create(panel.Dashboard{Name: "Original", Panels: []panel.Panel{{ID: "memory", Title: "Memory", Viz: "stat", Source: "sessions"}}}, Author{Kind: "user"}, "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.path(r.ID))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.update(r.ID, r.Version, Author{Kind: "user"}, "", func(current panel.Dashboard) (panel.Dashboard, error) {
		max := math.NaN()
		current.Panels[0].Max = &max
		return Apply(current, []Operation{{Op: "rename", Name: "Renamed"}})
	})
	var invalid *json.UnsupportedValueError
	if !errors.As(err, &invalid) {
		t.Errorf("edit lost clone error: %v", err)
	}
	after, err := os.ReadFile(s.path(r.ID))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("failed clone rewrote the dashboard")
	}
	kept, err := s.Get(r.ID)
	if err != nil || kept.Version != 1 || kept.Name != "Original" {
		t.Fatalf("failed clone changed revision: %+v %v", kept, err)
	}
}

func TestCloneIsIndependent(t *testing.T) {
	max := 100.0
	d := panel.Dashboard{Name: "Original", Variables: []panel.Variable{{Name: "agent", Field: "agent", Default: "claude"}}, Panels: []panel.Panel{{ID: "memory", Title: "Memory", Viz: "stat", Source: "sessions", Columns: []string{"title"}, Where: []panel.Filter{{Column: "agent", Value: "claude"}}, Max: &max, Grid: &panel.Grid{W: 6, H: 3}}}}
	before, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	cloned, err := clone(d)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d, cloned) {
		t.Fatalf("clone changed dashboard: %+v", cloned)
	}
	cloned.Name = "Changed"
	cloned.Variables[0].Default = "codex"
	cloned.Panels[0].Columns[0] = "age"
	cloned.Panels[0].Where[0].Value = "codex"
	*cloned.Panels[0].Max = 0
	cloned.Panels[0].Grid.W = 12
	after, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("mutating clone changed original dashboard")
	}
}

func TestRestoreChecksCurrentBase(t *testing.T) {
	s := New(t.TempDir())
	r, err := s.Create(panel.Dashboard{Name: "Original", Panels: []panel.Panel{{ID: "memory", Title: "Memory", Viz: "stat", Source: "sessions"}}}, Author{Kind: "user"}, "")
	if err != nil {
		t.Fatal(err)
	}
	latest, err := s.Edit(r.ID, []Operation{{Op: "rename", Name: "Current"}}, r.Version, Author{Kind: "user"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restore(r.ID, r.Version, r.Version, Author{Kind: "user"}); !errors.Is(err, ErrStale) {
		t.Fatalf("stale restore: %v", err)
	}
	kept, err := s.Get(r.ID)
	if err != nil || kept.Version != latest.Version || kept.Name != "Current" {
		t.Fatalf("stale restore wrote revision: %+v %v", kept, err)
	}
	restored, err := s.Restore(r.ID, r.Version, latest.Version, Author{Kind: "user"})
	if err != nil || restored.Version != latest.Version+1 || restored.Name != "Original" {
		t.Fatalf("current restore: %+v %v", restored, err)
	}
}

func TestSavedDashboardRejectsZeroBase(t *testing.T) {
	s := New(t.TempDir())
	spec := panel.Dashboard{Name: "Original", Panels: []panel.Panel{{ID: "memory", Title: "Memory", Viz: "stat", Source: "sessions"}}}
	r, err := s.Create(spec, Author{Kind: "user"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Replace(r.ID, spec, 0, Author{Kind: "user"}, ""); !errors.Is(err, ErrStale) {
		t.Errorf("replace accepted zero base: %v", err)
	}
	if _, err := s.Edit(r.ID, []Operation{{Op: "rename", Name: "Changed"}}, 0, Author{Kind: "user"}, ""); !errors.Is(err, ErrStale) {
		t.Errorf("edit accepted zero base: %v", err)
	}
	if _, err := s.Restore(r.ID, r.Version, 0, Author{Kind: "user"}); !errors.Is(err, ErrStale) {
		t.Errorf("restore accepted zero base: %v", err)
	}
	kept, err := s.Get(r.ID)
	if err != nil || kept.Version != r.Version || kept.Name != spec.Name {
		t.Fatalf("zero-base write changed revision: %+v %v", kept, err)
	}
}
