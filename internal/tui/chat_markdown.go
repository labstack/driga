package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/view"
)

// chatMarkdown renders only assistant text, after PlainCitations. Sanitize text
// before adding our styles so agent output cannot inject terminal sequences.
func chatMarkdown(text string, width int, noColor bool) string {
	width = max(1, width)
	var blocks []string
	for _, block := range chat.ParseMarkdown(text) {
		style := view.TokenStyle("ink", noColor)
		switch block.Kind {
		case "code":
			style = chatCodeStyle(style, noColor)
			prefix := "│ "
			if width < 3 {
				prefix = "│"
			}
			var lines []string
			for _, line := range strings.Split(block.Text, "\n") {
				line = clean(strings.ReplaceAll(line, "\t", "    "))
				// Code is never reflowed. Keep a visible rail even when
				// the terminal is too narrow for a character of code.
				line = ansi.Truncate(line, width-ansi.StringWidth(prefix), "…")
				lines = append(lines, style.Render(prefix+line))
			}
			blocks = append(blocks, strings.Join(lines, "\n"))
		case "ul", "ol":
			var items []string
			for i, item := range block.Items {
				prefix := "• "
				if block.Kind == "ol" {
					prefix = fmt.Sprintf("%d. ", i+1)
				}
				// Reserve a content cell; shrink the marker on tiny
				// terminals rather than overflowing the chat width.
				prefix = ansi.Truncate(prefix, max(1, width-1), "")
				indent := strings.Repeat(" ", ansi.StringWidth(prefix))
				body := chatMarkdownWrap(chatMarkdownSpans(item, style, noColor), width-ansi.StringWidth(prefix))
				if width == 1 {
					items = append(items, prefix+"\n"+body)
				} else {
					items = append(items, prefix+strings.ReplaceAll(body, "\n", "\n"+indent))
				}
			}
			blocks = append(blocks, strings.Join(items, "\n"))
		default:
			if block.Kind == "heading" {
				style = view.TokenStyle("accent", noColor).Bold(!noColor)
			}
			blocks = append(blocks, chatMarkdownWrap(chatMarkdownSpans(block.Spans, style, noColor), width))
		}
	}
	return strings.Join(blocks, "\n\n")
}

func chatCodeStyle(style lipgloss.Style, noColor bool) lipgloss.Style {
	if !noColor {
		style = style.Background(view.ThemeColor("sunk"))
	}
	return style
}

type chatMarkdownRun struct {
	text  string
	style lipgloss.Style
}

func chatMarkdownSpans(spans []chat.MarkdownSpan, style lipgloss.Style, noColor bool) []chatMarkdownRun {
	var runs []chatMarkdownRun
	for _, span := range spans {
		s := style
		switch span.Kind {
		case "strong":
			runs = append(runs, chatMarkdownSpans(span.Children, s.Bold(!noColor), noColor)...)
		case "em":
			runs = append(runs, chatMarkdownSpans(span.Children, s.Italic(!noColor), noColor)...)
		default:
			if span.Kind == "code" {
				s = chatCodeStyle(s, noColor)
			}
			// Inline code can contain newlines; clean each line as
			// plain chat does, retaining those explicit line breaks.
			lines := strings.Split(span.Text, "\n")
			for i := range lines {
				lines[i] = clean(lines[i])
			}
			runs = append(runs, chatMarkdownRun{text: strings.Join(lines, "\n"), style: s})
		}
	}
	return runs
}

func chatMarkdownWrap(runs []chatMarkdownRun, width int) string {
	width = max(1, width)
	var out strings.Builder
	column := 0
	for _, run := range runs {
		var fragment strings.Builder
		flush := func() {
			out.WriteString(run.style.Render(fragment.String()))
			fragment.Reset()
		}
		for text := run.text; text != ""; {
			if text[0] == '\n' {
				flush()
				out.WriteByte('\n')
				column = 0
				text = text[1:]
				continue
			}
			cluster, cells := ansi.FirstGraphemeCluster(text, ansi.GraphemeWidth)
			text = text[len(cluster):]
			if cells > width {
				cluster, cells = "…", 1
			}
			if column+cells > width {
				flush()
				out.WriteByte('\n')
				column = 0
			}
			fragment.WriteString(cluster)
			column += cells
		}
		flush()
	}
	// Style each fragment after wrapping, so every physical line has balanced
	// ANSI sequences even when the viewport starts in the middle of a span.
	return out.String()
}
