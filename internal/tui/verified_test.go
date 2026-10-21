package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestOrphansVerifiedColumnAndSnapshotFooter(t *testing.T) {
	s := fixture()
	s.snap.Mode = "snapshot"
	m := setup(t, s, Options{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 180, Height: 45})
	m = key(m, '4')
	text := m.View().Content
	if !strings.Contains(text, "VERIFIED") || !strings.Contains(text, "unverified (snapshot)") {
		t.Fatal(text)
	}
}
