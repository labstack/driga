package collector

import (
	"testing"

	"github.com/labstack/driga/internal/model"
)

func TestImmediateCwdLookupBoundedAndFair(t *testing.T) {
	d, st, p, a := setup(t)
	a.obs = nil
	p.procs = nil
	for i := int32(0); i < 205; i++ {
		p.procs = append(p.procs, model.Proc{PID: 1000 + i, PPID: 2, StartTime: d.Now(), Cmdline: "codex", Argv: []string{"codex"}})
	}
	seen := make(map[int32]bool)
	for range 2 {
		pass(t, d, st, Plan{Procs: true, Sockets: true})
		if len(p.cwdPIDs) > 200 {
			t.Fatalf("immediate lookup is unbounded: %d", len(p.cwdPIDs))
		}
		for _, pid := range p.cwdPIDs {
			seen[pid] = true
		}
	}
	if len(seen) != 205 {
		t.Fatalf("unresolved processes starved new lookups: %d", len(seen))
	}
}
