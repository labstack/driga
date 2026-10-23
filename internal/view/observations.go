package view

import (
	"fmt"
	"strings"

	"github.com/labstack/driga/internal/textfmt"
)

// ObservationSpark compresses the entire hour to fit a panel. Maxima preserve
// short bursts when several minute buckets share a terminal cell.
func ObservationSpark(buckets []int64, width int) string {
	width = min(len(buckets), max(0, width))
	if width == 0 {
		return ""
	}
	cells := make([]int64, width)
	var peak int64
	for i, n := range buckets {
		j := i * width / len(buckets)
		cells[j] = max(cells[j], n)
		peak = max(peak, n)
	}
	draw := make([]textfmt.SparkCell, len(cells))
	for i, n := range cells {
		draw[i].Value = float64(n)
	}
	return textfmt.Spark(draw, float64(peak), '·', textfmt.SparkLinear, nil)
}

// ObservationPath is SVG geometry only; callers keep all styling in static CSS.
func ObservationPath(buckets []int64) string {
	var peak int64
	for _, n := range buckets {
		peak = max(peak, n)
	}
	if peak == 0 {
		return ""
	}
	var b strings.Builder
	for i, n := range buckets {
		if n > 0 {
			fmt.Fprintf(&b, "M%d 10v-%.2f", i, 1+9*float64(n)/float64(peak))
		}
	}
	return b.String()
}

func (v FlowView) ObservationLogBuckets() []int64 {
	var total []int64
	add := func(n FlowNode) {
		if n.Observed == nil {
			return
		}
		if total == nil {
			total = make([]int64, 60)
		}
		for i, x := range n.Observed.LogBytesPerMin {
			if i < 60 {
				total[i] += x
			}
		}
	}
	if v.Lead != nil {
		add(*v.Lead)
	}
	for _, n := range v.Children {
		add(n)
	}
	return total
}
