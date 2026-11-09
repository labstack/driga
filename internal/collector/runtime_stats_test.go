package collector

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/store"
)

func TestRuntimeStatsPersistBoundedProcessSample(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { return writeRuntimeStats(tx, now) }); err != nil {
		t.Fatal(err)
	}
	raw, err := st.Meta("collector_runtime")
	if err != nil || len(raw) > 2048 {
		t.Fatalf("unbounded runtime sample: %d bytes, %v", len(raw), err)
	}
	got, err := ReadRuntimeStats(st, os.Getpid(), now, time.Minute)
	if err != nil || got.PID != os.Getpid() || !got.At.Equal(now) || got.GoSysBytes == 0 || got.GoReleasedBytes > got.GoSysBytes || got.Phase != "after_process_pass" {
		t.Fatalf("sample: %+v %v", got, err)
	}
}

type runtimeMeta string

func (m runtimeMeta) Meta(string) (string, error) { return string(m), nil }

func TestRuntimeStatsRejectUnattributedSample(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name string
		pid  int
		at   time.Time
	}{
		{"other process", 9, now}, {"stale", 42, now.Add(-2 * time.Minute)}, {"future", 42, now.Add(time.Second)},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, _ := json.Marshal(RuntimeStats{PID: test.pid, At: test.at, Phase: "after_process_pass"})
			if _, err := ReadRuntimeStats(runtimeMeta(raw), 42, now, time.Minute); err == nil {
				t.Fatal("unattributed runtime sample accepted")
			}
		})
	}
	if _, err := ReadRuntimeStats(runtimeMeta(strings.Repeat("x", 2049)), 42, now, time.Minute); err == nil {
		t.Fatal("oversized sample accepted")
	}
}

func BenchmarkRuntimeStats(b *testing.B) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := st.Tx(context.Background(), func(tx *store.Tx) error { return writeRuntimeStats(tx, now) }); err != nil {
			b.Fatal(err)
		}
	}
}
