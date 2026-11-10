package derive

import (
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

var benchmarkStateMixer model.StateMixer

// Every collector label pass retains four source rows. Include a live process
// and lifecycle evidence so the benchmark covers the normal collected shape.
func BenchmarkLabelWithEvidence(b *testing.B) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	stop := now.Add(-time.Minute)
	in := StateInput{
		Obs:  model.SessionObs{Adapter: "claude", PID: 42, RegistryStatus: "idle", LastActivityAt: stop},
		Proc: &model.Proc{PID: 42, CPUPercent: 0.1}, LastStop: &stop,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchmarkStateMixer = LabelWithEvidence(in, now, Thresholds{StuckAfter: 10 * time.Minute})
	}
}
