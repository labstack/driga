package command

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func runnerScope(t *testing.T) Scope {
	t.Helper()
	dirs := make([]string, 3)
	for i := range dirs {
		var e error
		dirs[i], e = os.MkdirTemp("", "dcs-")
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { os.RemoveAll(dirs[i]) })
	}
	return Scope{Project: dirs[0], Scratch: dirs[1], Denied: []string{dirs[2]}}
}
func prepareRun(t *testing.T, in Request, s Scope) Prepared {
	t.Helper()
	p, e := Prepare(context.Background(), in, s)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func realSandbox(t *testing.T) {
	t.Helper()
	c := ProbeSandbox(context.Background())
	t.Logf("sandbox: backend=%s OS=%s available=%t reason=%s", c.Backend, c.OSVersion, c.Available, c.Reason)
	if !c.Available {
		if os.Getenv("DRIGATEST_REQUIRE_SANDBOX") == "1" {
			t.Fatalf("required sandbox: %s", c.Reason)
		}
		t.Skipf("real sandbox unavailable: %s", c.Reason)
	}
}

func TestCommandRunnerBoundaries(t *testing.T) {
	if runtime.GOOS == "windows" {
		realSandbox(t)
	}
	s := runnerScope(t)
	if err := os.WriteFile(filepath.Join(s.Project, "read"), []byte("safe\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p := prepareRun(t, Request{Argv: []string{"cat", "read"}, Cwd: s.Project}, s)
	// Force only the unavailable path when the real probe is unavailable.
	var out, stderr bytes.Buffer
	r, e := Run(context.Background(), p, &out, &stderr)
	if e != nil || r.ExitCode != 0 || out.String() != "safe\n" {
		t.Fatalf("safe read: %#v %v %q", r, e, out.String())
	}
	secret := filepath.Join(s.Denied[0], "secret")
	if e := os.WriteFile(secret, []byte("secret"), 0600); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(s.Project, "escape")
	if e := os.Symlink(s.Denied[0], link); e == nil {
		p = prepareRun(t, Request{Argv: []string{"cat", "escape/secret"}, Cwd: s.Project}, s)
		if _, e = Run(context.Background(), p, &out, &stderr); e == nil {
			t.Fatal("protected symlink read")
		}
	}
	realSandbox(t)
	// The only host signalling target is this fixture's own disposable child.
	child := exec.Command("/bin/sleep", "30")
	if e = child.Start(); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	defer func() {
		_ = child.Process.Kill()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("fixture did not exit")
		}
	}()
	p = prepareRun(t, Request{Script: fmt.Sprintf("fixture_pid=%d; /bin/kill -TERM \"$fixture_pid\"", child.Process.Pid), Cwd: s.Project}, s)
	r, e = Run(context.Background(), p, &out, &stderr)
	if e == nil && r.ExitCode == 0 {
		t.Fatal("sandbox signalled host child")
	}
	select {
	case <-done:
		t.Fatal("host child exited after sandbox signal")
	default:
	}
	// These stores live in a private fake HOME inside the project. Reading them
	// through expansion must still fail in the actual OS sandbox, including
	// when the project would otherwise make the files writable/readable.
	fakeHome := filepath.Join(s.Project, "fixture-home")
	if e = os.MkdirAll(fakeHome, 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("HOME", fakeHome)
	for _, store := range []string{".claude", ".codex", ".ssh", ".aws", ".azure", ".config/gcloud", ".config/driga", ".local/share/driga", "Library/Keychains", "Library/Application Support/driga"} {
		dir := filepath.Join(fakeHome, store)
		if e = os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
		path := filepath.Join(dir, "fixture-secret")
		if e = os.WriteFile(path, []byte("fixture confidential"), 0600); e != nil {
			t.Fatal(e)
		}
		p = prepareRun(t, Request{Script: "fixture_path=" + quote(path) + "; cat \"$fixture_path\"", Cwd: s.Project}, s)
		out.Reset()
		r, e = Run(context.Background(), p, &out, &stderr)
		if e == nil && r.ExitCode == 0 || strings.Contains(out.String(), "fixture confidential") {
			t.Fatalf("protected store %s: %#v %v", store, r, e)
		}
	}
	claudeJSON := filepath.Join(fakeHome, ".claude.json")
	if e = os.WriteFile(claudeJSON, []byte("fixture confidential"), 0600); e != nil {
		t.Fatal(e)
	}
	p = prepareRun(t, Request{Script: "fixture_path=" + quote(claudeJSON) + "; cat \"$fixture_path\"", Cwd: s.Project}, s)
	out.Reset()
	r, e = Run(context.Background(), p, &out, &stderr)
	if e == nil && r.ExitCode == 0 || strings.Contains(out.String(), "fixture confidential") {
		t.Fatal("Claude sibling file read")
	}
	// Reach the OS boundary through expansion, so these assertions cannot be
	// satisfied by the literal-path classifier alone.
	for _, d := range s.Denied {
		p = prepareRun(t, Request{Script: "secret_dir=" + quote(d) + "; cat \"$secret_dir/secret\"", Cwd: s.Project}, s)
		out.Reset()
		r, e = Run(context.Background(), p, &out, &stderr)
		if e == nil && r.ExitCode == 0 || strings.Contains(out.String(), "secret") {
			t.Fatalf("kernel protected read: %#v %v %q", r, e, out.String())
		}
	}
	for _, target := range []string{filepath.Join(s.Project, "allowed"), filepath.Join(s.Scratch, "allowed")} {
		p = prepareRun(t, Request{Script: "printf allowed > '" + target + "'", Cwd: s.Project}, s)
		r, e = Run(context.Background(), p, &out, &stderr)
		if e != nil || r.ExitCode != 0 {
			t.Fatalf("allowed write: %#v %v %s", r, e, stderr.String())
		}
	}
	outside := filepath.Join(t.TempDir(), "outside")
	p = prepareRun(t, Request{Script: "printf bad > '" + outside + "'", Cwd: s.Project}, s)
	r, e = Run(context.Background(), p, &out, &stderr)
	if e == nil && r.ExitCode == 0 {
		t.Fatal("outside write succeeded")
	}
	if _, e = os.Stat(outside); !os.IsNotExist(e) {
		t.Fatalf("outside exists: %v", e)
	}
	if e = os.Symlink(filepath.Dir(outside), filepath.Join(s.Project, "outside-link")); e == nil {
		p = prepareRun(t, Request{Script: "printf bad > outside-link/outside", Cwd: s.Project}, s)
		r, e = Run(context.Background(), p, &out, &stderr)
		if e == nil && r.ExitCode == 0 {
			t.Fatal("symlink write escaped")
		}
		if _, e = os.Stat(outside); !os.IsNotExist(e) {
			t.Fatal("symlink created outside file")
		}
	}
	for _, d := range s.Denied {
		p = prepareRun(t, Request{Script: "cat '" + filepath.Join(d, "secret") + "'", Cwd: s.Project}, s)
		if _, e = Run(context.Background(), p, &out, &stderr); e == nil {
			t.Fatal("protected read accepted")
		}
	}
	t.Setenv("DRIGA_TEST_SECRET", "never inherit me")
	p = prepareRun(t, Request{Script: "env", Cwd: s.Project}, s)
	out.Reset()
	r, e = Run(context.Background(), p, &out, &stderr)
	if e != nil || r.ExitCode != 0 || strings.Contains(out.String(), "DRIGA_TEST_SECRET") {
		t.Fatalf("environment: %s %v", out.String(), e)
	}
}

func TestCommandRunnerNetwork(t *testing.T) {
	s := runnerScope(t)
	p := prepareRun(t, Request{Argv: []string{"ls"}, Cwd: s.Project}, s)
	changed := p
	changed.Parsed.Request.Network = true
	if _, e := Run(context.Background(), changed, &bytes.Buffer{}, &bytes.Buffer{}); e == nil {
		t.Fatal("network changed after preparation")
	}
	realSandbox(t)
	testSandboxNetwork(t, s)
}

func TestCommandRunnerUnavailable(t *testing.T) {
	s := runnerScope(t)
	p := prepareRun(t, Request{Argv: []string{"no-such-command"}, Cwd: s.Project}, s)
	// A prepared unavailable capability must never become unsandboxed execution.
	p.Sandbox = Capability{Reason: "missing backend"}
	p.Profile = ""
	p.seal = Digest(p)
	if _, e := Run(context.Background(), p, &bytes.Buffer{}, &bytes.Buffer{}); e == nil {
		t.Fatal("Other fell back")
	}
	if runtime.GOOS == "windows" && ProbeSandbox(context.Background()).Reason != "Running commands needs a sandbox, which Windows does not have yet." {
		t.Fatal("Windows refusal changed")
	}
	for _, args := range [][]string{{"git", "status"}, {"find", "."}, {"cat", "/etc/passwd"}, {"./ls"}, {"kill", "123"}} {
		p = prepareRun(t, Request{Argv: args, Cwd: s.Project}, s)
		p.Sandbox = Capability{Reason: "failing backend"}
		p.Profile = ""
		p.seal = Digest(p)
		if _, e := Run(context.Background(), p, &bytes.Buffer{}, &bytes.Buffer{}); e == nil {
			t.Fatalf("unsafe fallback: %v", args)
		}
	}
	p = prepareRun(t, Request{Argv: []string{"cat", "read"}, Cwd: s.Project}, s)
	p.Policy.Class = Read
	p.Parsed.Commands[0][0] = "sudo"
	p.seal = Digest(p)
	if _, e := Run(context.Background(), p, &bytes.Buffer{}, &bytes.Buffer{}); e == nil {
		t.Fatal("forged policy accepted")
	}
}

func TestCommandRunnerTimeout(t *testing.T) {
	s := runnerScope(t)
	p := prepareRun(t, Request{Argv: []string{"cat"}, Cwd: s.Project}, s)
	if p.Parsed.Request.TimeoutSeconds != 120 {
		t.Fatal("default")
	}
	if _, e := Prepare(context.Background(), Request{Argv: []string{"cat"}, Cwd: s.Project, TimeoutSeconds: 601}, s); e == nil {
		t.Fatal("limit")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Run(ctx, p, &bytes.Buffer{}, &bytes.Buffer{}); e == nil {
		t.Fatal("cancelled launch")
	}
	realSandbox(t)
	var out, stderr bytes.Buffer
	p = prepareRun(t, Request{Script: "printf out; printf err >&2; exit 7", Cwd: s.Project}, s)
	r, e := Run(context.Background(), p, &out, &stderr)
	if e != nil || r.ExitCode != 7 || out.String() != "out" || stderr.String() != "err" {
		t.Fatalf("streams/exit: %#v %v %q %q", r, e, out.String(), stderr.String())
	}
	p = prepareRun(t, Request{Script: "sleep 30 & wait", Cwd: s.Project, TimeoutSeconds: 1}, s)
	start := time.Now()
	r, e = Run(context.Background(), p, &out, &stderr)
	if e == nil || !r.TimedOut || time.Since(start) > 5*time.Second {
		t.Fatalf("timeout: %#v %v", r, e)
	}
	testSandboxDescendants(t, s)
}
