package collector

import (
	"fmt"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

// On a busy machine linkCodex ran Alive and path normalization for every
// unlinked session against every candidate process. Each process's liveness
// is a property of the process, so it is checked once per pass.
func TestLinkCodexChecksEachProcessOnce(t *testing.T) {
	now := time.Now()
	f := &fakePlatform{}
	for i := range 20 {
		f.procs = append(f.procs, model.Proc{PID: int32(1000 + i), Agent: "codex", Role: "interactive", Cwd: fmt.Sprintf("/work/p%d", i), StartTime: now.Add(-time.Hour)})
	}
	var ss []model.Session
	for i := range 200 {
		ss = append(ss, model.Session{SessionObs: model.SessionObs{Adapter: "codex", ExternalID: fmt.Sprint(i), Cwd: fmt.Sprintf("/work/p%d", i%40), StartedAt: now.Add(-30 * time.Minute)}})
	}
	linkCodex(ss, f.procs, f)
	if f.aliveCalls > len(f.procs) {
		t.Fatalf("Alive called %d times for %d processes", f.aliveCalls, len(f.procs))
	}
	linked := 0
	for _, s := range ss {
		if s.PID != 0 {
			linked++
		}
	}
	if linked != 20 {
		t.Fatalf("linked %d sessions, want 20 (one per live process)", linked)
	}
}
