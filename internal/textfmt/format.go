// Package textfmt shares display primitives across the CLI, TUI, and web.
package textfmt

import (
	"fmt"
	"strings"
)

// Bytes formats either compact terminal values or IEC web values.
func Bytes(n float64, compact bool) string {
	if compact {
		if n >= 1<<30 {
			return fmt.Sprintf("%.1fG", n/(1<<30))
		}
		return fmt.Sprintf("%.1fM", n/(1<<20))
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	i := 0
	for n >= 1024 && i < len(units)-1 {
		n /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", n, units[i])
}

// ShellQuote quotes one POSIX shell argument. force retains explicit quoting
// in copyable commands even for an otherwise safe word.
func ShellQuote(s string, force bool) string {
	if !force && s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/_-.:@", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// SparkCell carries an already aggregated bucket and its presentation metadata.
type SparkCell struct {
	Value   float64
	Missing bool
	Span    int
	Token   string
}

type SparkScale int

const (
	SparkLinear SparkScale = iota
	SparkCeil
	SparkFloor
)

// Spark draws all sparklines, retaining a dot for an unknown bucket. Callers
// supply aggregation, quantization, and styling to preserve their display scale.
func Spark(cells []SparkCell, peak float64, zero rune, scale SparkScale, render func(string, string) string) string {
	var out strings.Builder
	blocks := []rune("▁▂▃▄▅▆▇█")
	for _, cell := range cells {
		glyph := zero
		if cell.Missing {
			glyph = '·'
		} else if cell.Value > 0 && peak > 0 {
			level := 0
			switch scale {
			case SparkLinear:
				level = int(cell.Value / peak * 7)
			case SparkCeil:
				level = int((cell.Value*8 - 1) / peak)
			case SparkFloor:
				level = int(cell.Value/peak*8) - 1
			}
			glyph = blocks[min(7, max(0, level))]
		}
		text := strings.Repeat(string(glyph), max(1, cell.Span))
		if render != nil {
			text = render(cell.Token, text)
		}
		out.WriteString(text)
	}
	return out.String()
}
