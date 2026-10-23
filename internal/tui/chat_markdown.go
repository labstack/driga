package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/view"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
)

type markdownTerminal struct {
	source  []byte
	noColor bool
}

func chatMarkdown(text string, width int, noColor bool) string {
	d := chat.ParseMarkdown(text)
	r := markdownTerminal{d.Source, noColor}
	return r.blocks(d.Root, max(1, width))
}

func (r markdownTerminal) style(token string) lipgloss.Style {
	return view.TokenStyle(token, r.noColor)
}

func (r markdownTerminal) blocks(parent ast.Node, width int) string {
	var blocks []string
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		if body := r.block(n, width); body != "" {
			blocks = append(blocks, body)
		}
	}
	return strings.Join(blocks, "\n\n")
}

func (r markdownTerminal) block(node ast.Node, width int) string {
	base := r.style("ink")
	switch n := node.(type) {
	case *ast.Heading:
		s := r.style("md-heading").Bold(!r.noColor)
		if n.Level == 1 {
			s = s.Underline(!r.noColor)
		}
		if n.Level >= 3 {
			s = s.Italic(!r.noColor)
		}
		if n.Level >= 5 {
			s = s.Bold(false)
		}
		return chatMarkdownWrap(r.inline(n, s), width)
	case *ast.Paragraph, *ast.TextBlock:
		return chatMarkdownWrap(r.inline(n, base), width)
	case *ast.FencedCodeBlock:
		return r.code(string(n.Lines().Value(r.source)), clean(string(n.Language(r.source))), width)
	case *ast.CodeBlock:
		return r.code(string(n.Lines().Value(r.source)), "", width)
	case *ast.HTMLBlock:
		return chatMarkdownWrap([]chatMarkdownRun{{markdownClean(strings.TrimSuffix(string(n.Text(r.source)), "\n")), base}}, width)
	case *ast.Blockquote:
		prefix := "│ "
		if width <= 2 {
			prefix = "│"
		}
		body := r.blocks(n, max(1, width-ansi.StringWidth(prefix)))
		return markdownPrefix(body, r.style("md-quote").Render(prefix), width)
	case *ast.List:
		var items []string
		index := n.Start
		for item := n.FirstChild(); item != nil; item = item.NextSibling() {
			prefix := "• "
			if n.IsOrdered() {
				prefix = fmt.Sprintf("%d. ", index)
				index++
			}
			prefix = ansi.Truncate(prefix, max(1, width-1), "")
			body := r.blocks(item, max(1, width-ansi.StringWidth(prefix)))
			// A nested list follows its preceding paragraph on the next line.
			if n.IsTight {
				body = strings.ReplaceAll(body, "\n\n", "\n")
			}
			lines := strings.Split(body, "\n")
			if width == 1 {
				items = append(items, prefix+"\n"+body)
				continue
			}
			indent := strings.Repeat(" ", ansi.StringWidth(prefix))
			items = append(items, prefix+strings.Join(lines, "\n"+indent))
		}
		sep := "\n"
		if !n.IsTight {
			sep = "\n\n"
		}
		return strings.Join(items, sep)
	case *ast.ThematicBreak:
		return r.style("md-rule").Render(strings.Repeat("─", width))
	case *east.Table:
		return r.table(n, width)
	default:
		return r.blocks(n, width)
	}
}

func markdownClean(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = clean(lines[i])
	}
	return strings.Join(lines, "\n")
}

// Prefixes are fitted independently so even deeply nested content at one cell
// cannot overflow. Content wrapping always happens before styles are emitted.
func markdownPrefix(body, prefix string, width int) string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if width <= ansi.StringWidth(prefix) {
			out = append(out, ansi.Truncate(prefix, width, ""), line)
		} else {
			out = append(out, prefix+line)
		}
	}
	return strings.Join(out, "\n")
}

func (r markdownTerminal) code(body, language string, width int) string {
	prefix := "│ "
	if width < 3 {
		prefix = "│"
	}
	if width <= ansi.StringWidth(prefix) {
		prefix = ""
	}
	available := max(1, width-ansi.StringWidth(prefix))
	style := r.style("md-code")
	if !r.noColor {
		style = style.Background(view.ThemeColor("md-code-bg"))
	}
	var lines []string
	if language != "" {
		lines = append(lines, r.style("muted").Render(ansi.Truncate(language, width, "…")))
	}
	body = strings.TrimSuffix(body, "\n")
	for _, line := range strings.Split(body, "\n") {
		line = clean(strings.ReplaceAll(line, "\t", "    "))
		for {
			chunk := ansi.Truncate(line, available, "")
			if trimmed := strings.TrimRight(chunk, " "); trimmed != "" {
				chunk = trimmed
			}
			if chunk == "" && line != "" {
				cluster, _ := ansi.FirstGraphemeCluster(line, ansi.GraphemeWidth)
				chunk = cluster
			}
			lines = append(lines, style.Render(prefix+ansi.Truncate(chunk, available, "…")))
			line = strings.TrimPrefix(line, chunk)
			if line == "" {
				break
			}
		}
	}
	return strings.Join(lines, "\n")
}

func (r markdownTerminal) inline(parent ast.Node, style lipgloss.Style) []chatMarkdownRun {
	var runs []chatMarkdownRun
	add := func(text string, s lipgloss.Style) { runs = append(runs, chatMarkdownRun{markdownClean(text), s}) }
	for node := parent.FirstChild(); node != nil; node = node.NextSibling() {
		switch n := node.(type) {
		case *ast.Text:
			value := string(n.Value(r.source))
			if !n.IsRaw() {
				value = chat.MarkdownText(n.Value(r.source))
			}
			add(value, style)
			if n.HardLineBreak() {
				add("\n", style)
			} else if n.SoftLineBreak() {
				add(" ", style)
			}
		case *ast.String:
			value := string(n.Value)
			if !n.IsRaw() && !n.IsCode() {
				value = chat.MarkdownText(n.Value)
			}
			add(value, style)
		case *ast.Emphasis:
			s := style
			if n.Level == 2 {
				s = s.Bold(!r.noColor)
			} else {
				s = s.Italic(!r.noColor)
			}
			runs = append(runs, r.inline(n, s)...)
		case *east.Strikethrough:
			s := style.Strikethrough(!r.noColor)
			if !r.noColor {
				s = s.Foreground(view.ThemeColor("md-strike"))
			}
			runs = append(runs, r.inline(n, s)...)
		case *ast.CodeSpan:
			s := style
			if !r.noColor {
				s = s.Foreground(view.ThemeColor("md-code")).Background(view.ThemeColor("md-code-bg"))
			}
			add(strings.ReplaceAll(string(n.Text(r.source)), "\n", " "), s)
		case *ast.Link:
			s := style
			if !r.noColor {
				s = s.Foreground(view.ThemeColor("md-link"))
			}
			runs = append(runs, r.inline(n, s)...)
			destination := chat.MarkdownText(n.Destination)
			if chat.MarkdownText(n.Text(r.source)) != destination {
				add(" ("+destination+")", r.style("muted"))
			}
		case *ast.AutoLink:
			label, destination := chat.MarkdownText(n.Label(r.source)), chat.MarkdownText(n.URL(r.source))
			add(label, r.style("md-link"))
			if label != destination {
				add(" ("+destination+")", r.style("muted"))
			}
		case *ast.Image:
			add("[image: "+chat.MarkdownText(n.Text(r.source))+"]", style)
			add(" ("+chat.MarkdownText(n.Destination)+")", r.style("muted"))
		case *east.TaskCheckBox:
			mark := "☐"
			if n.IsChecked {
				mark = "☑"
			}
			add(mark+" ", r.style("md-task"))
		case *ast.RawHTML:
			add(string(n.Segments.Value(r.source)), style)
		default:
			runs = append(runs, r.inline(n, style)...)
		}
	}
	return runs
}

// Tables shrink the widest column first, retaining at least four display cells.
// If the minimum grid cannot fit, each row becomes a readable Header: value block.
func (r markdownTerminal) table(n *east.Table, width int) string {
	var rows [][][]chatMarkdownRun
	columns := len(n.Alignments)
	if columns == 0 {
		return ""
	}
	sizes := make([]int, columns)
	for row := n.FirstChild(); row != nil; row = row.NextSibling() {
		cells := make([][]chatMarkdownRun, columns)
		i := 0
		for cell := row.FirstChild(); cell != nil && i < columns; cell = cell.NextSibling() {
			style := r.style("ink")
			if len(rows) == 0 {
				style = style.Bold(!r.noColor)
			}
			cells[i] = r.inline(cell, style)
			for _, line := range strings.Split(chatMarkdownWrap(cells[i], chat.MaxText), "\n") {
				sizes[i] = max(sizes[i], ansi.StringWidth(line))
			}
			i++
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		return ""
	}
	total := 3 * (columns - 1)
	for i := range sizes {
		sizes[i] = max(4, sizes[i])
		total += sizes[i]
	}
	for total > width {
		widest := -1
		for i, size := range sizes {
			if size > 4 && (widest < 0 || size > sizes[widest]) {
				widest = i
			}
		}
		if widest < 0 {
			break
		}
		sizes[widest]--
		total--
	}
	if total > width {
		var out []string
		start := 1
		if len(rows) == 1 {
			start = 0
		}
		for _, row := range rows[start:] {
			var fields []string
			for i, cell := range row {
				label := ansi.Strip(chatMarkdownWrap(rows[0][i], chat.MaxText))
				runs := []chatMarkdownRun{{label + ": ", r.style("ink").Bold(!r.noColor)}}
				runs = append(runs, cell...)
				fields = append(fields, chatMarkdownWrap(runs, width))
			}
			out = append(out, strings.Join(fields, "\n"))
		}
		return strings.Join(out, "\n\n")
	}
	border := r.style("md-table-border")
	separator := border.Render(" │ ")
	var out []string
	for rowIndex, row := range rows {
		lines := make([][]string, columns)
		height := 1
		for i, cell := range row {
			lines[i] = strings.Split(chatMarkdownWrap(cell, sizes[i]), "\n")
			height = max(height, len(lines[i]))
		}
		for y := 0; y < height; y++ {
			cells := make([]string, columns)
			for i := range cells {
				value := ""
				if y < len(lines[i]) {
					value = lines[i][y]
				}
				padding := max(0, sizes[i]-ansi.StringWidth(value))
				left := 0
				switch n.Alignments[i] {
				case east.AlignRight:
					left = padding
				case east.AlignCenter:
					left = padding / 2
				}
				cells[i] = strings.Repeat(" ", left) + value + strings.Repeat(" ", padding-left)
			}
			out = append(out, strings.Join(cells, separator))
		}
		if rowIndex == 0 {
			rules := make([]string, columns)
			for i, size := range sizes {
				rules[i] = strings.Repeat("─", size)
			}
			out = append(out, border.Render(strings.Join(rules, "─┼─")))
		}
	}
	return strings.Join(out, "\n")
}

type chatMarkdownRun struct {
	text  string
	style lipgloss.Style
}

// Wrap graphemes before emitting styles. Words span AST/style boundaries; each
// physical line closes its ANSI styles so viewport slicing cannot leak them.
func chatMarkdownWrap(runs []chatMarkdownRun, width int) string {
	width = max(1, width)
	type cell struct {
		text         string
		width, style int
	}
	var cells []cell
	for i, run := range runs {
		for text := run.text; text != ""; {
			cluster, w := ansi.FirstGraphemeCluster(text, ansi.GraphemeWidth)
			text = text[len(cluster):]
			if w > width {
				cluster, w = "…", 1
			}
			cells = append(cells, cell{cluster, w, i})
		}
	}
	var out, fragment strings.Builder
	column, styleIndex := 0, -1
	flush := func() {
		if fragment.Len() > 0 {
			out.WriteString(runs[styleIndex].style.Render(fragment.String()))
			fragment.Reset()
		}
	}
	newline := func() { flush(); out.WriteByte('\n'); column = 0 }
	emit := func(c cell) {
		if column+c.width > width {
			newline()
		}
		if styleIndex != c.style {
			flush()
			styleIndex = c.style
		}
		fragment.WriteString(c.text)
		column += c.width
	}
	var spaces []cell
	for i := 0; i < len(cells); {
		c := cells[i]
		if c.text == "\n" {
			newline()
			spaces = nil
			i++
			continue
		}
		if c.text == " " {
			spaces = append(spaces, c)
			i++
			continue
		}
		end, wordWidth := i, 0
		for end < len(cells) && cells[end].text != " " && cells[end].text != "\n" {
			wordWidth += cells[end].width
			end++
		}
		if column > 0 && column+len(spaces)+wordWidth > width {
			newline()
			spaces = nil
		}
		if column > 0 {
			for _, space := range spaces {
				emit(space)
			}
		}
		spaces = nil
		for ; i < end; i++ {
			emit(cells[i])
		}
	}
	flush()
	return out.String()
}
