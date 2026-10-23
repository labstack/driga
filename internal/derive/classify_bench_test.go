package derive

import (
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
)

func BenchmarkFindOrphans(b *testing.B) {
	sigs := signatures.Builtin()
	now := time.Unix(1792499400, 0)
	procs := make([]model.Proc, 1000)
	for i := range procs {
		cmd := "/usr/bin/unrelated --worker"
		if i%4 == 0 {
			cmd = "node /opt/agent/mcp-server.js"
		}
		procs[i] = model.Proc{PID: int32(i + 2), PPID: 1, StartTime: now.Add(-time.Hour), Cmdline: cmd}
	}
	idx := NewProcIndex(procs)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		FindOrphans(idx, nil, sigs, now, time.Minute, nil)
	}
}
