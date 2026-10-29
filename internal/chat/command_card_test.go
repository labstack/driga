package chat

import (
	"fmt"
	"github.com/labstack/driga/internal/command"
	"github.com/labstack/driga/internal/model"
	"strings"
	"testing"
	"time"
)

func TestCommandCardWords(t *testing.T) {
	p := command.Prepared{Parsed: command.Parsed{Request: command.Request{Argv: []string{"pkill", "-f", "maestro.cli.AppKt mcp"}, Cwd: "/work/project", TimeoutSeconds: 120}}, Scope: command.Scope{Project: "/work/project", Scratch: "/scratch"}, Policy: command.Policy{Class: command.Process}, ProcessSnapshot: &command.ProcessSnapshot{}}
	for i := 0; i < 65; i++ {
		p.ProcessSnapshot.Targets = append(p.ProcessSnapshot.Targets, command.ProcessTarget{Proc: model.Proc{PID: int32(100 + i), Exe: "/usr/bin/java", Cmdline: "maestro.cli.AppKt mcp", Argv: []string{"java", "maestro.cli.AppKt mcp"}, StartTime: time.Unix(100, 0), Footprint: 5_000_000_000 / 65}, OwnerKnown: true, OwnerUID: 501})
	}
	c := PresentCommand(p)
	if c.Summary != "Stop 65 processes owned by you matching `maestro.cli.AppKt mcp` (about 5 GB)" {
		t.Fatal(c.Summary)
	}
	if len(c.Targets) != 65 || c.Targets[64].PID != 164 || c.Targets[0].Owner != "UID 501" || c.Targets[0].StartedAt.IsZero() || c.Targets[0].Executable != "/usr/bin/java" {
		t.Fatalf("identities: %+v", c.Targets)
	}
	if c.Cwd != "/work/project" || fmt.Sprint(c.Requested.Argv) != "[pkill -f maestro.cli.AppKt mcp]" {
		t.Fatal(c)
	}
	p.Policy.Class = command.Other
	p.ProcessSnapshot = nil
	p.Sandbox.Reason = "sandbox unavailable"
	if c = PresentCommand(p); c.Error != "sandbox unavailable" || !strings.Contains(c.Boundary, "writes only to /work/project") || !strings.Contains(c.Boundary, "no network") {
		t.Fatal(c)
	}
}

func TestCommandCardEffectiveInvocation(t *testing.T) {
	p := command.Prepared{Parsed: command.Parsed{Request: command.Request{Script: "echo exact > result.txt", Cwd: "/project"}}, Scope: command.Scope{Project: "/project", Scratch: "/scratch"}, Policy: command.Policy{Class: command.Other, Commands: []command.Executable{{Path: "/bin/echo", Argv: []string{"echo", "exact"}}}}, Sandbox: command.Capability{Available: true}}
	c := PresentCommand(p)
	if len(c.Effective) != 1 || len(c.Effective[0]) != 3 || c.Effective[0][0] != "/bin/sh" || c.Effective[0][2] != "echo exact > result.txt" {
		t.Fatal("script launch differs from card", c.Effective)
	}
	p.Policy.Class = command.Process
	p.ProcessSnapshot = &command.ProcessSnapshot{}
	c = PresentCommand(p)
	if len(c.Effective) != 0 {
		t.Fatal("process card claims to launch selector", c.Effective)
	}
	p.Policy.Class = command.Read
	p.ProcessSnapshot = nil
	p.Sandbox.Available = false
	c = PresentCommand(p)
	if strings.HasPrefix(c.Boundary, "sandboxed:") {
		t.Fatal("unavailable read claims a sandbox", c.Boundary)
	}
}

func TestCommandCardDisclosesWorktreeWriteRoots(t *testing.T) {
	p := command.Prepared{WritableRoots: []string{"/project", "/scratch", "/main/.git"}, Sandbox: command.Capability{Available: true}}
	if got := PresentCommand(p).Boundary; !strings.Contains(got, "/main/.git") {
		t.Fatal("common Git directory missing from write boundary", got)
	}
}
