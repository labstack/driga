package view

import (
	"testing"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
)

func TestProcTreeToleratesStartSkew(t *testing.T) {
	start := time.Unix(1700000000, 0)
	idx := derive.NewProcIndex([]model.Proc{
		{PID: 10, PPID: 1, StartTime: start, RSS: 100},
		{PID: 11, PPID: 10, StartTime: start.Add(time.Second), RSS: 50},
	})
	s := model.Session{SessionObs: model.SessionObs{PID: 10, ProcStart: start.Add(-300 * time.Millisecond)}}
	if got := len(procTree(s, idx)); got != 2 {
		t.Fatalf("registry start 300ms off: got %d procs, want 2", got)
	}
	s.ProcStart = start.Add(-5 * time.Second)
	if got := len(procTree(s, idx)); got != 0 {
		t.Fatalf("pid reuse (5s off): got %d procs, want 0", got)
	}
}
