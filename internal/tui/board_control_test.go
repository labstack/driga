package tui

import (
	"fmt"
	"strings"
	"testing"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestBoardSessionTextControls(t *testing.T) {
	for _, field := range []string{"title", "branch", "project", "adapter", "last tool"} {
		for _, width := range []int{80, 140, 200} {
			for _, noColor := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%d/plain=%t", field, width, noColor), func(t *testing.T) {
					m := boardApp(t, "quiet")
					m.width, m.height, m.opts.NoColor = width, 50, noColor
					m.snap.BoardData.Nodes = m.snap.BoardData.Nodes[:1]
					n := &m.snap.BoardData.Nodes[0]
					n.Title, n.Branch, n.Project, n.Adapter, n.StartedFrom = "main", "main", "dockyard", "codex", ""
					dirty := "main\t界\r\x00\x07\x1b\x7f\u0085\nend"
					text := &n.Title
					switch field {
					case "branch":
						n.Title = ""
						text = &n.Branch
					case "project":
						text = &n.Project
					case "adapter":
						text = &n.Adapter
					case "last tool":
						text = &n.LastTool
					}
					*text = clean(dirty)
					wantBody, _ := m.boardLayout()
					wantFrame := m.View().Content
					*text = dirty
					body, _ := m.boardLayout()
					frame := m.View().Content
					for _, rendered := range []string{body, frame} {
						if !noColor {
							rendered = ansi.Strip(rendered)
						}
						for i, line := range strings.Split(rendered, "\n") {
							if strings.ContainsFunc(line, unicode.IsControl) {
								t.Errorf("line %d contains controls: %q", i, line)
							}
							if got := lipgloss.Width(line); got > width {
								t.Errorf("line %d width = %d, want <= %d: %q", i, got, width, line)
							}
						}
					}
					if width >= 120 {
						cardLines := 0
						for _, line := range strings.Split(ansi.Strip(body), "\n") {
							if !strings.HasSuffix(line, "│") && !strings.HasSuffix(line, "╮") && !strings.HasSuffix(line, "╯") {
								continue
							}
							cardLines++
							cardWidth := min(52, width/3)
							card := ansi.Cut(line, width-cardWidth, width)
							if got, want := lipgloss.Width(card), cardWidth; got != want {
								t.Errorf("card line width = %d, want %d: %q", got, want, card)
							}
							if !strings.HasPrefix(card, "│") && !strings.HasPrefix(card, "╭") && !strings.HasPrefix(card, "╰") {
								t.Errorf("card border shifted: %q", card)
							}
						}
						if cardLines < 3 || !strings.Contains(body, "Selected family") {
							t.Fatal("selected-family card missing")
						}
					}
					if body != wantBody || frame != wantFrame {
						t.Error("controls changed layout compared with space-normalized session text")
					}
				})
			}
		}
	}
}
