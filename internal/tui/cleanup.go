package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/labstack/driga/internal/cleanup"
)

func (m *app) loadCleanup() tea.Cmd {
	run, ctx := m.opts.CleanupList, m.opts.Context
	m.busy = true
	return func() tea.Msg { cs, err := run(ctx); return cleanupMsg{cs, err} }
}
func cleanupDetail(c cleanup.Candidate) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s\n%s\n%s\n", c.ID, c.Status, c.Path, strings.Join(c.Evidence, "\n"))
	prefix := ""
	if c.SizeCapped {
		prefix = "≥"
	}
	fmt.Fprintf(&b, "Size: %s%d bytes\n", prefix, c.Size)
	for _, i := range c.Ignored {
		class := "non-regenerable; requires explicit --include-ignored"
		if i.Regenerable {
			class = "regenerable"
		}
		fmt.Fprintf(&b, "Ignored: %s · %s · %d bytes\n", i.Path, class, i.Size)
	}
	return b.String()
}
