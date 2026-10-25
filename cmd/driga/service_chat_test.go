package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Chat runs in the Driga service. A question asked from the CLI reaches the
// service's broker, which starts claude, which starts Driga's MCP server: that
// server must be the driga CLI, because a program started as drigad is the
// service. A stand-in claude completes the MCP handshake and calls a tool.
func TestChatThroughTheService(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix sockets and ps")
	}
	if testing.Short() {
		t.Skip("builds and runs the driga binary")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "driga")
	for _, build := range [][]string{{"-o", bin, "."}, {"-o", filepath.Join(dir, "fake", "claude"), "./testdata/fakeclaude"}} {
		cmd := exec.Command("go", append([]string{"build"}, build...)...)
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %v: %v: %s", build, err, out)
		}
	}
	daemon := filepath.Join(dir, "drigad")
	if err := os.Link(bin, daemon); err != nil {
		t.Fatal(err)
	}
	n := newNode(t, bin, "local")
	write(t, n.path("config.toml"), "[fleet]\nserve = false\n[intake]\notlp = false\n[inbox]\nalerts = \"off\"\n")
	rec := t.TempDir()
	env := func(cmd *exec.Cmd) *exec.Cmd {
		cmd.Env = append(cmd.Env, "PATH="+filepath.Join(dir, "fake")+":/usr/bin:/bin:/usr/sbin:/sbin", "FAKE_CLAUDE_REC="+rec)
		return cmd
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := env(n.cmd(ctx))
	service.Path, service.Args = daemon, []string{daemon}
	var log logBuffer
	service.Stdout, service.Stderr = &log, &log
	if err := service.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = service.Wait() }()

	var answer string
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		parent, _ := os.ReadFile(filepath.Join(rec, "parent"))
		if strings.Contains(string(parent), "drigad") {
			break
		}
		askCtx, cancelAsk := context.WithTimeout(ctx, 30*time.Second)
		out, _ := env(n.cmd(askCtx, "ask", "Tell me about memory used right now")).CombinedOutput()
		cancelAsk()
		answer = string(out)
		time.Sleep(500 * time.Millisecond)
	}
	parent, _ := os.ReadFile(filepath.Join(rec, "parent"))
	if !strings.Contains(string(parent), "drigad") {
		t.Fatalf("claude was not started by the service (parent %q); ask said %s\nservice log:\n%s", parent, answer, log.String())
	}
	if !strings.Contains(answer, "Memory used:") {
		t.Fatalf("Chat did not answer through the MCP server: %s\nservice log:\n%s", answer, log.String())
	}
}
