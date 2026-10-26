package platform

import (
	"github.com/labstack/driga/internal/model"
	"testing"
	"time"
)

func TestMachineCPUPercentInterval(t *testing.T) {
	at := time.Date(2026, 3, 14, 15, 0, 0, 0, time.UTC)
	prev := model.MachineSample{TS: at, CPUTotalSeconds: 1000, CPUBusySeconds: 400}
	cur := model.MachineSample{TS: at.Add(5 * time.Second), CPUTotalSeconds: 1040, CPUBusySeconds: 410}
	if got := MachineCPUPercent(prev, cur); got == nil || *got != 25 {
		t.Fatalf("busy share = %v, want 25%% across all cores", got)
	}
	for _, p := range []model.MachineSample{{}, {TS: at, CPUTotalSeconds: 2000, CPUBusySeconds: 700}, {TS: cur.TS, CPUTotalSeconds: 1000, CPUBusySeconds: 400}} {
		if got := MachineCPUPercent(p, cur); got != nil {
			t.Fatalf("invalid baseline produced %v", *got)
		}
	}
	cur.CPUBusySeconds = 400
	if got := MachineCPUPercent(prev, cur); got == nil || *got != 0 {
		t.Fatalf("idle must be a measured zero: %v", got)
	}
}
