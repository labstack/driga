package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
)

func TestHookCapturesParentProcessIdentity(t *testing.T) {
	for _, agent := range []string{"claude", "codex"} {
		t.Run(agent, func(t *testing.T) {
			dir := hookTestEnv(t)
			old := hookParentProcess
			t.Cleanup(func() { hookParentProcess = old })
			start := time.Unix(50, 0).UTC()
			hookParentProcess = func() (model.Proc, error) {
				return model.Proc{PID: 123, StartTime: start}, nil
			}
			if err := (HookCmd{Adapter: agent, Event: "SessionStart"}).Run(&Globals{Stdin: strings.NewReader(`{"session_id":"hook-only"}`)}); err != nil {
				t.Fatal(err)
			}
			events, _, err := spool.Drain(dir, nil)
			if err != nil || len(events) != 1 {
				t.Fatal(events, err)
			}
			if events[0].PID != 123 || !events[0].ProcStart.Equal(start) {
				t.Fatalf("parent identity missing from spool: %+v", events[0])
			}
		})
	}
}
