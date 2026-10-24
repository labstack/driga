package attention

import (
	"fmt"
	"strings"
)

// DecimalBytes matches the decimal disk thresholds documented in the config.
func DecimalBytes(n uint64) string {
	for _, unit := range []struct {
		size uint64
		name string
	}{{1_000_000_000_000, "TB"}, {1_000_000_000, "GB"}, {1_000_000, "MB"}, {1_000, "kB"}} {
		if n >= unit.size {
			return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/float64(unit.size)), ".0") + " " + unit.name
		}
	}
	return fmt.Sprintf("%d B", n)
}
