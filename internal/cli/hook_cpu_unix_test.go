//go:build !windows

package cli

import (
	"github.com/labstack/driga/internal/spool"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestHookCommandFastAndExitZero(t *testing.T) {
	dir := hookTestEnv(t)
	cmd := HookCmd{Adapter: "claude", Event: "Stop"}
	if err := cmd.Run(&Globals{Stdin: strings.NewReader("invalid JSON")}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "hook-errors.log"))
	if err != nil || info.Size() == 0 || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	// Process CPU excludes scheduler delays and filesystem wait on loaded hosts.
	// Several runs reduce runtime/GC noise; the hook still has a strict CPU budget.
	cpuTime := func() time.Duration {
		var usage syscall.Rusage
		if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
			t.Fatal(err)
		}
		return time.Duration(usage.Utime.Nano() + usage.Stime.Nano())
	}
	costs := make([]time.Duration, 5)
	for i := range costs {
		before := cpuTime()
		if err := cmd.Run(&Globals{Stdin: strings.NewReader(`{"session_id":"s","cwd":"/repo","last_assistant_message":"private text"}`)}); err != nil {
			t.Fatal(err)
		}
		costs[i] = cpuTime() - before
	}
	slices.Sort(costs)
	if !testing.Short() && !raceEnabled && costs[len(costs)/2] >= 20*time.Millisecond {
		t.Fatalf("median hook CPU time %v", costs[len(costs)/2])
	}

	got, _, err := spool.Drain(dir, nil)
	if err != nil || len(got) != 5 || got[0].SessionID != "s" || got[0].Message != "" || got[0].Event != "Stop" {
		t.Fatal(got, err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "blocked"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DRIGA_SPOOL_DIR", filepath.Join(filepath.Dir(dir), "blocked"))
	if err := cmd.Run(&Globals{Stdin: strings.NewReader(`{"session_id":"s"}`)}); err != nil {
		t.Fatal(err)
	}
}
