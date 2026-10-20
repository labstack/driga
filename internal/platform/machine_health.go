package platform

import (
	"encoding/binary"
	"errors"
	"math"
	"strconv"
	"strings"
)

// rusage_info_v2, sys/resource.h: 16-byte UUID then uint64 counters;
// ri_phys_footprint is at byte 72. Require the complete flavor (176 bytes).
func parseFootprint(b []byte) (uint64, error) {
	if len(b) != 176 {
		return 0, errors.New("invalid rusage_info_v2 size")
	}
	return binary.LittleEndian.Uint64(b[72:80]), nil
}

// ParseVmSwap reads Linux /proc/<pid>/status, whose memory sizes are KiB.
func ParseVmSwap(b []byte) uint64 {
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 || f[0] != "VmSwap:" || f[2] != "kB" {
			continue
		}
		n, err := strconv.ParseUint(f[1], 10, 64)
		if err == nil && n <= math.MaxUint64/1024 {
			return n * 1024
		}
	}
	return 0
}

// ParseCompressed reads vm_stat's compressor's physical pages, not the
// uncompressed logical pages stored there; it uses the reported page size.
func ParseCompressed(b []byte) (uint64, error) {
	var size uint64
	for _, line := range strings.Split(string(b), "\n") {
		if _, rest, ok := strings.Cut(line, "page size of "); ok {
			v, _, _ := strings.Cut(rest, " ")
			size, _ = strconv.ParseUint(v, 10, 64)
		}
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "Pages occupied by compressor:"); ok {
			n, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(rest), "."), 10, 64)
			if err == nil && size > 0 && n <= math.MaxUint64/size {
				return n * size, nil
			}
		}
	}
	return 0, errors.New("compressed memory unavailable")
}
