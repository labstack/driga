package tui

import (
	"fmt"
	"strings"

	"github.com/labstack/driga/internal/textfmt"

	"charm.land/lipgloss/v2"
	"github.com/NimbleMarkets/ntcharts/v2/barchart"
	"github.com/labstack/driga/internal/view"
)

func (m *app) eachAgent(fn func(view.AgentRow)) {
	var walk func([]view.AgentRow)
	walk = func(rows []view.AgentRow) {
		for _, r := range rows {
			if m.machineVisible(r.MachineID) {
				fn(r)
			}
			walk(r.Children)
		}
	}
	walk(m.snap.Agents.Rows)
}

// panel keeps detail panes consistent with the shared rounded frames.
func panel(title, body string, width int) string { return framedPanel(title, "", body, width, true) }

func framedPanel(title, hint, body string, width int, noColor bool) string {
	width = max(4, width)
	inner := width - 4
	border := view.TokenStyle("line", noColor)
	heading := " " + truncate(clean(title), max(1, width-6)) + " "
	hint = clean(hint)
	room := width - 2 - lipgloss.Width(heading) - 2
	right := ""
	if room > 4 && hint != "" {
		right = " " + truncate(hint, room-2) + " "
	}
	headingStyle := view.TokenStyle("ink", noColor)
	if !noColor {
		headingStyle = headingStyle.Bold(true)
	}
	top := border.Render("╭") + headingStyle.Render(heading) + border.Render(strings.Repeat("─", max(0, width-2-lipgloss.Width(heading)-lipgloss.Width(right)))) + view.TokenStyle("faint", noColor).Render(right) + border.Render("╮")
	lines := []string{top}
	for _, line := range strings.Split(body, "\n") {
		line = truncate(line, inner)
		lines = append(lines, border.Render("│")+" "+line+strings.Repeat(" ", max(0, inner-lipgloss.Width(line)))+" "+border.Render("│"))
	}
	lines = append(lines, border.Render("╰"+strings.Repeat("─", max(0, width-2))+"╯"))
	return strings.Join(lines, "\n")
}

func (m *app) stateText(text string, label ...string) string {
	state := text
	if len(label) > 0 {
		state = label[0]
	} else if fields := strings.Fields(text); len(fields) > 0 {
		state = fields[0]
	}
	return view.StateStyle(state, m.opts.NoColor).Render(text)
}

func bar(value, total float64, width int, style lipgloss.Style) string {
	b := barchart.New(max(1, width), 1, barchart.WithHorizontalBars(), barchart.WithNoAxis(), barchart.WithNoAutoMaxValue(), barchart.WithMaxValue(max(1, total)), barchart.WithBarGap(0))
	b.Push(barchart.BarData{Values: []barchart.BarValue{{Value: min(value, total), Style: style}}})
	b.Draw()
	return b.View()
}

// gaugeBar draws the full capacity, including the unused track at zero usage.
func gaugeBar(used, total uint64, width int, noColor bool) string {
	width = max(1, width)
	filled := min(width, int(view.GaugePercent(used, total)*float64(width)/100))
	track := strings.Repeat("░", width-filled)
	if !noColor {
		track = lipgloss.NewStyle().Faint(true).Render(track)
	}
	return view.GaugeStyle(used, total, noColor).Render(strings.Repeat("█", filled)) + track
}

// activityMinuteColumn maps a minute to its first column, or its peak bin.
// Extra columns go to the oldest minutes first.
func activityMinuteColumn(minute, minutes, width int) int {
	if width >= minutes {
		return minute*(width/minutes) + min(minute, width%minutes)
	}
	return ((minute+1)*width - 1) / minutes
}

// activitySpark fills the width with minute blocks, binning peaks when space is tight.
// A bin containing a missing minute remains a dim dot rather than inventing history.
func activitySpark(buckets []view.ActivityBucket, width int, load bool) string {
	return styledSpark(buckets, width, load, true)
}

func styledSpark(buckets []view.ActivityBucket, width int, load, noColor bool) string {
	count := min(max(1, width), len(buckets))
	ceiling := 1.0
	for _, b := range buckets {
		if load && b.Sample != nil {
			ceiling = max(ceiling, b.Sample.Load1, float64(b.Sample.Cores))
		} else if !load {
			ceiling = max(ceiling, float64(b.Events))
		}
	}
	cells := make([]textfmt.SparkCell, 0, count)
	for x := 0; x < count; x++ {
		from, to := x*len(buckets)/count, (x+1)*len(buckets)/count
		span := activityMinuteColumn(x+1, count, max(1, width)) - activityMinuteColumn(x, count, max(1, width))
		value, valid, critical := 0.0, true, false
		for _, b := range buckets[from:to] {
			if load {
				if b.Sample == nil || load && b.Sample.Load1 < 0 {
					valid = false
					continue
				}
				value = max(value, b.Sample.Load1)
				critical = critical || b.Sample.Cores > 0 && b.Sample.Load1 > float64(b.Sample.Cores)
			} else {
				if b.Sample == nil && b.Events == 0 {
					valid = false
				}
				value = max(value, float64(b.Events))
			}
		}
		if !valid {
			cells = append(cells, textfmt.SparkCell{Missing: true, Span: span, Token: "faint"})
			continue
		}
		token := "idle"
		if load {
			token = "ink"
			if critical {
				token = "crit"
			}
		}
		cells = append(cells, textfmt.SparkCell{Value: value, Span: span, Token: token})
	}
	return textfmt.Spark(cells, ceiling, '▁', textfmt.SparkFloor, func(token, text string) string { return view.TokenStyle(token, noColor).Render(text) })
}

func activityRow(buckets []view.ActivityBucket, width int, load, noColor bool) string {
	label, current, peak, token := "events  ", "—", 0.0, "idle"
	for _, b := range buckets {
		if load {
			if b.Sample != nil {
				peak = max(peak, b.Sample.Load1)
			}
		} else {
			peak = max(peak, float64(b.Events))
		}
	}
	if len(buckets) > 0 {
		last := buckets[len(buckets)-1]
		if load {
			if last.Sample != nil {
				current = view.FormatLoad(last.Sample.Load1, "%.1f")
				if last.Sample.Cores > 0 && last.Sample.Load1 > float64(last.Sample.Cores) {
					token = "crit"
				}
			}
		} else if last.Sample != nil || last.Events > 0 {
			current = fmt.Sprintf("%d/min", last.Events)
		}
	}
	summary := fmt.Sprintf("%s max %.0f", current, peak)
	if load {
		label = "load    "
		if token != "crit" {
			token = "ink"
		}
		summary = fmt.Sprintf("%s max %.1f", current, peak)
	}
	// Reserve the same value column for both rows and the shared time axis.
	spark := styledSpark(buckets, max(1, width-8-max(18, lipgloss.Width(summary))-1), load, noColor)
	return view.TokenStyle("faint", noColor).Render(label) + spark + " " + view.TokenStyle(token, noColor).Render(summary)
}

func (m *app) splitDetail() bool { return m.tab == 0 && m.width >= 140 && !m.detail }

func (m *app) agentsSplitView() string {
	left := m.width / 2
	right := m.width - left - 3
	text := "Select an agent"
	if r, ok := m.selected(); ok {
		if d, found := m.snap.AgentDetails[r.agent]; found {
			text = m.styledAgentDetail(d, max(1, right-4))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(left).Render(m.table.View()), " │ ", panel("Agent detail", text, right))
}

func (m *app) styledAgentDetail(d view.AgentView, width int) string {
	return m.styleAgentState(agentDetail(d, width), d.Label)
}

func (m *app) styleAgentState(text, label string) string {
	return strings.Replace(text, "State: "+clean(label), "State: "+view.StateStyle(label, m.opts.NoColor).Render(clean(label)), 1)
}
