package platform

import (
	"encoding/binary"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

func TestRecordedFootprint(t *testing.T) {
	b, err := os.ReadFile("testdata/rusage-v2.hex")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseFootprint(raw)
	if err != nil || got != 987654321 {
		t.Fatalf("footprint %d: %v", got, err)
	}
	for _, n := range []int{0, 79, 175} {
		if _, err := parseFootprint(raw[:n]); err == nil {
			t.Fatalf("accepted truncated rusage %d", n)
		}
	}
	binary.LittleEndian.PutUint64(raw[72:80], 0)
	if got, err := parseFootprint(raw); err != nil || got != 0 {
		t.Fatalf("zero footprint %d %v", got, err)
	}
}

func TestRecordedProcSwap(t *testing.T) {
	b, err := os.ReadFile("testdata/status-memory")
	if err != nil {
		t.Fatal(err)
	}
	if got := ParseVmSwap(b); got != 42*1024 {
		t.Fatal(got)
	}
	for _, b := range []string{"VmSwap: -1 kB", "VmSwap: 18446744073709551615 kB", "VmSwap: 10 MB", "Name: x"} {
		if ParseVmSwap([]byte(b)) != 0 {
			t.Fatal(b)
		}
	}
}

func TestRecordedCompressed(t *testing.T) {
	b, err := os.ReadFile("testdata/vm-stat.txt")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseCompressed(b)
	if err != nil || got != 123*16384 {
		t.Fatalf("%d %v", got, err)
	}
	if _, err := ParseCompressed([]byte("Pages occupied by compressor: 123.")); err == nil {
		t.Fatal("missing page size")
	}
}
