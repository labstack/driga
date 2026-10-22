package hooks

import (
	"fmt"
	"strings"
)

type diffLine struct {
	kind byte
	text string
}

// lineEdits finds a shortest edit script using Myers' diagonal frontier.
// Equal lines include their newline, so EOF newline changes are real edits.
func lineEdits(before, after []string) []diffLine {
	n, m := len(before), len(after)
	// Creation/deletion previews need no frontier or quadratic trace.
	if n == 0 || m == 0 {
		edits := make([]diffLine, 0, n+m)
		for _, line := range before {
			edits = append(edits, diffLine{'-', line})
		}
		for _, line := range after {
			edits = append(edits, diffLine{'+', line})
		}
		return edits
	}
	frontier := map[int]int{1: 0}
	var trace []map[int]int
	for d := 0; d <= n+m; d++ {
		next := make(map[int]int, 2*d+1)
		for k := -d; k <= d; k += 2 {
			x := frontier[k-1] + 1
			if k == -d || k != d && frontier[k-1] < frontier[k+1] {
				x = frontier[k+1]
			}
			y := x - k
			for x < n && y < m && before[x] == after[y] {
				x++
				y++
			}
			next[k] = x
			if x >= n && y >= m {
				trace = append(trace, frontier)
				var reversed []diffLine
				for depth := d; depth >= 0; depth-- {
					prev := trace[depth]
					k := x - y
					pk := k - 1
					if k == -depth || k != depth && prev[k-1] < prev[k+1] {
						pk = k + 1
					}
					px, py := prev[pk], prev[pk]-pk
					for x > px && y > py {
						x--
						y--
						reversed = append(reversed, diffLine{' ', before[x]})
					}
					if depth == 0 {
						break
					}
					if x == px {
						y--
						reversed = append(reversed, diffLine{'+', after[y]})
					} else {
						x--
						reversed = append(reversed, diffLine{'-', before[x]})
					}
				}
				for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
					reversed[i], reversed[j] = reversed[j], reversed[i]
				}
				return reversed
			}
		}
		trace = append(trace, frontier)
		frontier = next
	}
	return nil
}

// Unified renders a minimal line diff with three context lines and hunk ranges.
func (d Diff) Unified() string {
	if !d.Changed || d.Before == d.After {
		return ""
	}
	lines := func(s string) []string {
		if s == "" {
			return nil
		}
		parts := strings.SplitAfter(s, "\n")
		if parts[len(parts)-1] == "" {
			parts = parts[:len(parts)-1]
		}
		return parts
	}
	edits := lineEdits(lines(d.Before), lines(d.After))
	var b strings.Builder
	if d.FullRewrite {
		b.WriteString("# Full settings rewrite: targeted edit unavailable\n")
	}
	fmt.Fprintf(&b, "--- %s\n+++ %s\n", d.Path, d.Path)
	oldLine, newLine := 1, 1
	for i := 0; i < len(edits); {
		first := i
		for first < len(edits) && edits[first].kind == ' ' {
			first++
		}
		if first == len(edits) {
			break
		}
		start := max(i, first-3)
		for ; i < start; i++ {
			oldLine++
			newLine++
		}
		end := first + 1
		for j := first + 1; j < len(edits); j++ {
			if edits[j].kind != ' ' {
				end = j + 1
			}
			if j-end >= 6 {
				break
			}
		}
		end = min(len(edits), end+3)
		oldCount, newCount := 0, 0
		for _, e := range edits[start:end] {
			if e.kind != '+' {
				oldCount++
			}
			if e.kind != '-' {
				newCount++
			}
		}
		oldStart, newStart := oldLine, newLine
		if oldCount == 0 {
			oldStart--
		}
		if newCount == 0 {
			newStart--
		}
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
		for _, e := range edits[start:end] {
			fmt.Fprintf(&b, "%c%s", e.kind, e.text)
			if !strings.HasSuffix(e.text, "\n") {
				b.WriteString("\n\\ No newline at end of file\n")
			}
		}
		oldLine += oldCount
		newLine += newCount
		i = end
	}
	return b.String()
}
