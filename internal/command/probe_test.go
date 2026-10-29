package command

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestProbeDiagnostics(t *testing.T) {
	c := ProbeSandbox(context.Background())
	t.Logf("sandbox: backend=%s OS=%s available=%t reason=%s", c.Backend, c.OSVersion, c.Available, c.Reason)
	if !c.Available && c.Reason == "" {
		t.Fatal("unavailable sandbox has no diagnostic")
	}
}

func TestProbeStderr(t *testing.T) {
	if len(os.Args) > 1 && os.Args[len(os.Args)-1] == "driga-probe-helper" {
		fmt.Fprintln(os.Stderr, "bwrap: --disable-userns requires --unshare-user ghp_fixtureTokenSecret")
		fmt.Fprintln(os.Stderr, strings.Repeat("detail ", 10000))
		os.Exit(1)
	}
	cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestProbeStderr$", "driga-probe-helper")
	c := probeCommand(Capability{Backend: "fixture"}, cmd)
	if c.Available || !strings.Contains(c.Reason, "exit status 1") || !strings.Contains(c.Reason, "--disable-userns requires --unshare-user") {
		t.Fatal(c)
	}
	if strings.Contains(c.Reason, "ghp_") || !strings.Contains(c.Reason, "[redacted]") || len([]rune(c.Reason)) > 256 {
		t.Fatalf("unbounded or unscrubbed diagnostic: %q", c.Reason)
	}
	var w probeStderr
	n, err := w.Write([]byte(strings.Repeat("x", 100000)))
	if err != nil || n != 100000 || len(w.data) != 16*1024 {
		t.Fatal(n, err, len(w.data))
	}
}
