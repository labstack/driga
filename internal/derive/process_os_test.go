package derive

import (
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
)

func TestSubreaperEvidenceUsesSampleOS(t *testing.T) {
	now := time.Unix(1700000000, 0)
	ps := []model.Proc{
		{PID: 600, PPID: 600, StartTime: now.Add(-3 * time.Hour)},
		{PID: 700, PPID: 600, StartTime: now.Add(-2 * time.Hour), Cmdline: "systemd --user"},
		{PID: 701, PPID: 700, StartTime: now.Add(-time.Hour), Cmdline: "claude mcp serve"},
		{PID: 702, PPID: 701, StartTime: now.Add(-time.Minute)},
	}
	for _, sourceOS := range []string{"linux", "darwin", "windows"} {
		t.Run(sourceOS, func(t *testing.T) {
			got := FindOrphans(NewProcIndexForOS(ps, sourceOS), nil, signatures.Builtin(), now, 0, nil, WithSubreaper(700))
			if sourceOS == "windows" {
				if len(got) != 0 {
					t.Fatalf("live Windows parent treated as subreaper: %+v", got)
				}
				return
			}
			if len(got) != 1 || got[0].RootPID != 701 || got[0].ProcCount != 2 ||
				!strings.Contains(strings.Join(got[0].Evidence, ";"), "reparented to systemd --user") {
				t.Fatalf("Unix sample lost subreaper evidence: %+v", got)
			}
		})
	}
}
