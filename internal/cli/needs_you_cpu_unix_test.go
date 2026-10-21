//go:build !windows

package cli

import (
	"bytes"
	"github.com/labstack/driga/internal/spool"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCodexPermissionHookFastExit(t *testing.T) {
	dir := hookTestEnv(t)
	var out bytes.Buffer
	cpu := func() time.Duration {
		var u syscall.Rusage
		if err := syscall.Getrusage(syscall.RUSAGE_SELF, &u); err != nil {
			t.Fatal(err)
		}
		return time.Duration(u.Utime.Nano() + u.Stime.Nano())
	}
	costs := make([]time.Duration, 5)
	for i := range costs {
		start := cpu()
		if err := (HookCmd{Adapter: "codex", Event: "PermissionRequest"}).Run(&Globals{Stdin: strings.NewReader(`{"session_id":"s","tool_name":"Bash","tool_input":{"command":"private"}}`), Stdout: &out}); err != nil {
			t.Fatal(err)
		}
		costs[i] = cpu() - start
	}
	slices.Sort(costs)
	if !testing.Short() && !raceEnabled && costs[2] >= 20*time.Millisecond {
		t.Fatalf("Codex median hook CPU %s", costs[2])
	}
	got, _, err := spool.Drain(dir, nil)
	if err != nil || len(got) != 5 || out.Len() != 0 {
		t.Fatal(got, err, out.String())
	}
}
