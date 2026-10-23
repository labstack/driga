package collector

import (
	"context"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/store"
)

// Measure collection, orphan planning and persistence together, with stable
// process identities and no subprocesses or adapters.
func BenchmarkProcessPass(b *testing.B) {
	now := time.Unix(1792499400, 0)
	p := &fakePlatform{procs: make([]model.Proc, 1000)}
	for i := range p.procs {
		cmd := "/usr/bin/unrelated --worker"
		if i%4 == 0 {
			cmd = "npx some-mcp-server"
		}
		p.procs[i] = model.Proc{PID: int32(i + 2), PPID: 1, StartTime: now.Add(-time.Hour), Cmdline: cmd}
	}
	st, err := store.Open("", store.Memory)
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()
	d := Deps{Cfg: config.Default(), Plat: p, Sigs: signatures.Builtin(), SpoolDir: b.TempDir(), State: NewState(), Now: func() time.Time { return now }}
	if _, err := Pass(context.Background(), d, st, Plan{Procs: true}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := Pass(context.Background(), d, st, Plan{Procs: true}); err != nil {
			b.Fatal(err)
		}
	}
}
