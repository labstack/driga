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

// DecimalBytesFloor truncates instead of rounding, for values shown against a
// limit: 19.96 GB free must not read as "20 GB free, below 20 GB".
func DecimalBytesFloor(n uint64) string {
	for _, size := range []uint64{1_000_000_000_000, 1_000_000_000, 1_000_000, 1_000} {
		if n >= size {
			return DecimalBytes(n / (size / 10) * (size / 10))
		}
	}
	return DecimalBytes(n)
}
