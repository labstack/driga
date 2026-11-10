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
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { return writeRuntimeStats(tx, now, "current") }); err != nil {
		t.Fatal(err)
	}
	raw, err := st.Meta("collector_runtime")
	if err != nil || len(raw) > 2048 {
		t.Fatalf("unbounded runtime sample: %d bytes, %v", len(raw), err)
	}
	got, err := ReadRuntimeStats(st, Owner{PID: os.Getpid(), Instance: "current"}, now, time.Minute)
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
			raw, _ := json.Marshal(RuntimeStats{PID: test.pid, Instance: "current", At: test.at, Phase: "after_process_pass"})
			if _, err := ReadRuntimeStats(runtimeMeta(raw), Owner{PID: 42, Instance: "current"}, now, time.Minute); err == nil {
				t.Fatal("unattributed runtime sample accepted")
			}
		})
	}
	for _, instance := range []string{"", "prior-run"} {
		raw, _ := json.Marshal(RuntimeStats{PID: 42, Instance: instance, At: now, Phase: "after_process_pass"})
		if _, err := ReadRuntimeStats(runtimeMeta(raw), Owner{PID: 42, Instance: "current"}, now, time.Minute); err == nil {
			t.Fatalf("sample from prior collector instance %q accepted for reused PID", instance)
		}
	}
	if _, err := ReadRuntimeStats(runtimeMeta(strings.Repeat("x", 2049)), Owner{PID: 42, Instance: "current"}, now, time.Minute); err == nil {
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
		if err := st.Tx(context.Background(), func(tx *store.Tx) error { return writeRuntimeStats(tx, now, "current") }); err != nil {
			b.Fatal(err)
		}
	}
}
