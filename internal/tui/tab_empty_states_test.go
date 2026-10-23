package tui

import (
	"strings"
	"testing"
)

func TestEmptyStatesExplainTabAndFilter(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	for tab := 0; tab <= 5; tab++ {
		m.tab = tab
		m.rows = nil
		if got := m.View().Content; strings.Contains(got, "No items") || !strings.Contains(got, "No ") {
			t.Fatal(tab, got)
		}
		m.filter.SetValue("no-match")
		if got := m.View().Content; !strings.Contains(got, "No matching items") {
			t.Fatal(tab, got)
		}
		m.filter.SetValue("")
	}
}
