package tail

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/model"
)

func TestScanMatchesRead(t *testing.T) {
	for _, content := range []string{
		"first\nsecond\npartial",
		strings.Repeat("x", 128<<10) + "\nlast\n",
		"first\n" + strings.Repeat("x", MaxLine+1) + "\nlast\npartial",
	} {
		path := writeLog(t, content)
		for _, budget := range []int64{0, 1, 1 << 20, 8 << 20} {
			var prev model.Offset
			for range 8 {
				want := mustRead(t, path, prev, budget)
				var lines [][]byte
				got, err := Scan(context.Background(), path, prev, budget, func(line []byte, offset int64) {
					if string(line) != content[offset:offset+int64(len(line))] || content[offset+int64(len(line))] != '\n' {
						t.Fatalf("incorrect absolute offset: %d", offset)
					}
					lines = append(lines, append([]byte{}, line...))
				})
				if err != nil || len(got.Lines) != 0 {
					t.Fatalf("scan: %+v, %v", got, err)
				}
				got.Lines = lines
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("budget %d: scan differs from read (offsets %+v, %+v)", budget, got.Next, want.Next)
				}
				if prev.Pos == got.Next.Pos {
					break
				}
				prev = got.Next
			}
		}
	}
}

func TestScanCancellation(t *testing.T) {
	path := writeLog(t, "first\nsecond\n")
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	r, err := Scan(ctx, path, model.Offset{}, 1024, func([]byte, int64) {
		calls++
		cancel()
	})
	if !errors.Is(err, context.Canceled) || calls != 1 || r.Next.Pos != 6 {
		t.Fatalf("canceled scan: calls=%d, next=%+v, err=%v", calls, r.Next, err)
	}
	// Cancellation returns the shared buffer lease, allowing later scans.
	_, err = Scan(context.Background(), path, r.Next, 1024, func([]byte, int64) { calls++ })
	if err != nil || calls != 2 {
		t.Fatalf("scan after cancellation: calls=%d, err=%v", calls, err)
	}
}
