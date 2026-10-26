package dashboard

import (
	"reflect"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/panel"
)

func TestUpdateClearsOptionalFields(t *testing.T) {
	max := 100.0
	before := panel.Panel{
		ID: "memory", Title: "Memory", Viz: "stat", Source: "sessions", Description: "Help",
		Where: []panel.Filter{{Column: "agent", Value: "claude"}}, By: "agent", Measure: "sum:mem_bytes",
		Columns: []string{"title"}, Sort: "-age", Limit: 20, Unit: "bytes", Max: &max, Total: "max:mem_bytes", Empty: "No sessions",
		Width: 6, Height: "l", Content: "Markdown", Grid: &panel.Grid{X: 0, Y: 0, W: 6, H: 3},
	}
	current := panel.Dashboard{Name: "Mine", Panels: []panel.Panel{before}}
	fields := []string{"description", "where", "by", "measure", "columns", "sort", "limit", "unit", "max", "total", "empty", "width", "height", "content", "grid"}
	after, err := Apply(current, []Operation{{Op: "update", ID: "memory", Clear: fields}})
	if err != nil {
		t.Fatal(err)
	}
	want := panel.Panel{ID: "memory", Title: "Memory", Viz: "stat", Source: "sessions"}
	if !reflect.DeepEqual(after.Panels[0], want) {
		t.Fatalf("clear: %+v", after.Panels[0])
	}
	if !reflect.DeepEqual(current.Panels[0], before) {
		t.Fatal("clear mutated original dashboard")
	}
	// Clearing wins over a simultaneous typed patch. Zero pointer values remain
	// expressible without clear, unlike scalar zero values in a typed patch.
	zero := 0.0
	after, err = Apply(current, []Operation{{Op: "update", ID: "memory", Panel: &panel.Panel{Description: "new", Max: &zero, Limit: 10}, Clear: []string{"description", "limit"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := after.Panels[0]; got.Description != "" || got.Limit != 0 || got.Max == nil || *got.Max != 0 || got.Title != "Memory" || got.Width != 6 {
		t.Fatalf("merge then clear: %+v", got)
	}
}

func TestUpdateRejectsInvalidClearFields(t *testing.T) {
	current := panel.Dashboard{Name: "Mine", Panels: []panel.Panel{{ID: "memory", Title: "Memory", Viz: "stat", Source: "sessions"}}}
	for _, field := range []string{"id", "title", "viz", "source", "not_a_field", "Limit"} {
		t.Run(field, func(t *testing.T) {
			_, err := Apply(current, []Operation{{Op: "update", ID: "memory", Panel: &panel.Panel{}, Clear: []string{field}}})
			if err == nil || !strings.Contains(err.Error(), "cannot clear panel field") {
				t.Fatalf("clear %s: %v", field, err)
			}
		})
	}
	if _, err := Apply(current, []Operation{{Op: "rename", Name: "New", Clear: []string{"description"}}}); err == nil || !strings.Contains(err.Error(), "clear is only supported by update") {
		t.Fatalf("clear outside update: %v", err)
	}
}

func TestMoveBeforeSelfIsExplicit(t *testing.T) {
	current := panel.Dashboard{Name: "Mine", Panels: []panel.Panel{{ID: "memory", Title: "Memory", Viz: "stat", Source: "sessions"}}}
	_, err := Apply(current, []Operation{{Op: "move", ID: "memory", Before: "memory"}})
	if err == nil || !strings.Contains(err.Error(), "cannot move a panel before itself") {
		t.Fatalf("move before self: %v", err)
	}
}
