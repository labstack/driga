//go:build e2e

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/chat"
	commandpolicy "github.com/labstack/driga/internal/command"
)

type commandStep struct {
	Tool    string `json:"tool"`
	Input   any    `json:"input"`
	Barrier string `json:"barrier,omitempty"`
	Replay  bool   `json:"replay,omitempty"`
}
type commandHarness struct {
	n               node
	c               *chat.Remote
	ctx             context.Context
	project, helper string
}
type commandChild struct {
	cmd   *exec.Cmd
	in    io.WriteCloser
	lines chan string
	done  chan struct{}
}

func (h commandHarness) spawn(t *testing.T, pattern, behavior string) *commandChild {
	t.Helper()
	cmd := exec.Command(h.helper, pattern, behavior)
	cmd.Env = h.n.cmd(h.ctx).Env
	in, e := cmd.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	cmd.Stderr = os.Stderr
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	c := &commandChild{cmd: cmd, in: in, lines: make(chan string, 4), done: make(chan struct{})}
	go func() {
		s := bufio.NewScanner(out)
		for s.Scan() {
			c.lines <- s.Text()
		}
		cmd.Wait()
		close(c.done)
	}()
	t.Cleanup(func() {
		in.Close()
		select {
		case <-c.done:
			return
		default:
		}
		_ = cmd.Process.Kill()
		select {
		case <-c.done:
		case <-time.After(10 * time.Second):
			t.Error("fixture child was not reaped")
		}
	})
	c.ready(t)
	return c
}
func (c *commandChild) ready(t *testing.T) {
	t.Helper()
	select {
	case line := <-c.lines:
		if line != "ready" {
			t.Fatalf("fixture barrier %q", line)
		}
	case <-c.done:
		t.Fatal("fixture exited before ready")
	case <-time.After(10 * time.Second):
		t.Fatal("fixture ready timeout")
	}
}
func (c *commandChild) alive(t *testing.T) {
	t.Helper()
	if _, e := fmt.Fprintln(c.in, "ping"); e != nil {
		t.Fatal("fixture liveness write", e)
	}
	select {
	case line := <-c.lines:
		if line != "alive" {
			t.Fatalf("fixture liveness barrier %q", line)
		}
	case <-c.done:
		t.Fatalf("fixture PID %d unexpectedly exited", c.cmd.Process.Pid)
	case <-time.After(10 * time.Second):
		t.Fatalf("fixture PID %d did not answer liveness ping", c.cmd.Process.Pid)
	}
}
func (c *commandChild) exited(t *testing.T) {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(10 * time.Second):
		t.Fatalf("fixture PID %d survived", c.cmd.Process.Pid)
	}
}
func (h commandHarness) send(t *testing.T, mode chat.Mode, steps ...commandStep) string {
	t.Helper()
	c, e := h.c.Create(h.ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = h.c.SetMode(h.ctx, c.ID, mode); e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(steps)
	if e != nil {
		t.Fatal(e)
	}
	if e = h.c.Send(h.ctx, c.ID, "CHAT_COMMAND_FIXTURE\n"+string(raw), chat.Context{Project: h.project}); e != nil {
		t.Fatal(e)
	}
	return c.ID
}
func (h commandHarness) until(t *testing.T, id string, pred func(chat.State) bool) chat.State {
	t.Helper()
	ctx, cancel := context.WithTimeout(h.ctx, 90*time.Second)
	defer cancel()
	s, e := h.c.Read(ctx, id, 0)
	for e == nil {
		if pred(s) {
			return s
		}
		if !s.Active && s.Queued == 0 {
			t.Fatalf("turn ended before expected state: %+v; result %s", s, s.Result)
		}
		_, e = h.c.Wait(ctx, id, s.Cursor)
		if e == nil {
			s, e = h.c.Read(ctx, id, 0)
		}
	}
	t.Fatal(e)
	return s
}
func (h commandHarness) pending(t *testing.T, id string, after int) chat.Action {
	t.Helper()
	s := h.until(t, id, func(s chat.State) bool { return len(s.Actions) > after && s.Actions[after].Status == "pending" })
	return s.Actions[after]
}
func (h commandHarness) idle(t *testing.T, id string) chat.State {
	t.Helper()
	s := h.until(t, id, func(s chat.State) bool { return !s.Active && s.Queued == 0 })
	// A successful fake-Claude final event proves the MCP round trip completed.
	if len(s.Result) == 0 || string(s.Result) == "null" {
		t.Fatalf("fake Claude did not finish: %+v", s.Messages)
	}
	return s
}
func (h commandHarness) decide(t *testing.T, id string, a chat.Action, allow bool) {
	t.Helper()
	if e := h.c.Decide(h.ctx, id, a.ID, allow); e != nil {
		t.Fatal(e)
	}
}
func commandRequest(argv ...string) commandStep {
	return commandStep{Tool: "run_command", Input: commandpolicy.Request{Argv: argv}}
}
func assertTargets(t *testing.T, a chat.Action, children ...*commandChild) {
	t.Helper()
	if a.Command == nil || a.Command.Class != commandpolicy.Process || a.Command.Count != len(children) || len(a.Command.Targets) != len(children) {
		t.Fatalf("snapshot size/class: %+v", a)
	}
	want := map[int32]bool{}
	for _, c := range children {
		want[int32(c.cmd.Process.Pid)] = true
	}
	for _, p := range a.Command.Targets {
		if !want[p.PID] {
			t.Fatalf("unowned or duplicate PID %d on card", p.PID)
		}
		delete(want, p.PID)
		if p.Owner == "" || p.Owner == "unknown owner" || p.StartedAt.IsZero() || p.Executable == "" || p.CommandLine == "" || len(p.Argv) == 0 {
			t.Fatalf("incomplete displayed identity: %+v", p)
		}
	}
	if a.GrantEligible {
		t.Fatal("process offered a grant")
	}
}

// Follow cmd/driga's built-binary/service fixture pattern; internal/e2e is
// Windows-specific. Every process signalled by these cases is a direct fixture
// child, never a host agent. Cleanup waits only on exec.Cmd values we started.
func TestChatCommandsBuiltBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds fixture binaries and real driga")
	}
	dir := t.TempDir()
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	bin := filepath.Join(dir, "driga"+ext)
	helper := filepath.Join(dir, "fixture-process"+ext)
	fake := filepath.Join(dir, "fake", "claude"+ext)
	for _, b := range []struct{ output, path string }{{bin, "."}, {helper, "./testdata/chat-command-process"}, {fake, "./testdata/chat-command-claude"}} {
		cmd := exec.Command("go", "build", "-o", b.output, b.path)
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("build %s: %v %s", b.path, e, out)
		}
	}
	n := newNode(t, bin, "commands")
	project, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	git(t, project, "init", "-b", "main")
	write(t, filepath.Join(project, "read.txt"), "fixture read output\n")
	// Register a selected project using the real collector, then run the service.
	rootJSON, _ := json.Marshal(filepath.Dir(project))
	write(t, n.path("config.toml"), "capture_text = 'snippets'\nroots = ["+string(rootJSON)+"]\n[fleet]\nserve = false\n[intake]\notlp = false\n[inbox]\nalerts = 'off'\n[adapters.claude]\nenabled = false\n[adapters.codex]\nenabled = false\n[adapters.copilot]\nenabled = false\n[adapters.gemini]\nenabled = false\n")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	// The PATH contains only our fake agent plus OS tools, never host agent CLIs.
	path := filepath.Join(dir, "fake") + string(os.PathListSeparator) + "/usr/bin:/bin:/usr/sbin:/sbin"
	if runtime.GOOS == "windows" {
		path = filepath.Join(dir, "fake") + ";" + filepath.Join(os.Getenv("SystemRoot"), "System32") + ";" + os.Getenv("SystemRoot")
		gitExe, err := exec.LookPath("git")
		if err != nil {
			t.Fatal(err)
		}
		path += ";" + filepath.Dir(gitExe)
	}
	cmdFor := func(args ...string) *exec.Cmd {
		cmd := n.cmd(ctx, args...)
		cmd.Env = append(cmd.Env, "PATH="+path)
		return cmd
	}
	if out, e := cmdFor("service", "run", "--once").CombinedOutput(); e != nil {
		t.Fatalf("collect project: %v %s", e, out)
	}
	service := cmdFor("service", "run")
	var log logBuffer
	service.Stdout = &log
	service.Stderr = &log
	if e = service.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { cancel(); service.Wait() }()
	var remote *chat.Remote
	// Retry a readiness probe, never infer readiness from elapsed time.
	deadline := time.NewTimer(time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for remote == nil {
		remote, e = chat.Dial(ctx, n.path("data"))
		if e == nil {
			break
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatalf("broker not ready: %v\n%s", e, log.String())
		}
	}
	defer remote.Close()
	h := commandHarness{n: n, c: remote, ctx: ctx, project: project, helper: helper}
	if os.Getenv("DRIGATEST_REQUIRE_SANDBOX") == "1" {
		if cap := commandpolicy.ProbeSandbox(ctx); !cap.Available {
			t.Fatalf("required sandbox: %s", cap.Reason)
		}
	}
	t.Run("snapshot-65", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("pkill -f snapshot proof is Unix; literal Windows case below")
		}
		pattern := fmt.Sprintf("chat-command-owned-%d", time.Now().UnixNano())
		var children []*commandChild
		for range 65 {
			children = append(children, h.spawn(t, pattern, "normal"))
		}
		id := h.send(t, chat.Confirm, commandRequest("pkill", "-f", pattern))
		a := h.pending(t, id, 0)
		assertTargets(t, a, children...)
		if !strings.Contains(a.Command.Summary, "Stop 65 processes owned by you matching `"+pattern+"`") {
			t.Fatal(a.Command.Summary)
		}
		late := h.spawn(t, pattern, "normal")
		for _, c := range children {
			c.alive(t)
		}
		h.decide(t, id, a, true)
		s := h.idle(t, id)
		for _, c := range children {
			c.exited(t)
		}
		late.alive(t)
		got := s.Actions[0]
		if got.Status != "done" || len(got.Command.Stopped) != 65 || len(got.Command.Vanished) != 0 || len(got.Command.Skipped) != 0 || len(got.Command.Survivors) != 0 || got.Result != "Stopped 65, vanished 0, skipped 0, survivors 0" {
			t.Fatalf("snapshot results: %+v", got)
		}
		for _, p := range got.Command.Stopped {
			if p.Reason != "stopped" || p.PID == int32(late.cmd.Process.Pid) {
				t.Fatal(p)
			}
		}
		if e := h.c.Decide(ctx, id, a.ID, true); e == nil {
			t.Fatal("Allow replay accepted")
		}
		found := false
		for _, m := range s.Messages {
			if m.Role == "action" && strings.Contains(m.Text, got.Result) {
				found = true
			}
		}
		if !found {
			t.Fatal("missing recorded result categories")
		}
	})
	t.Run("all-outcomes-and-second-force-card", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("exec-in-place identity fixture is Unix; native Windows force below")
		}
		pattern := fmt.Sprintf("chat-command-outcomes-%d", time.Now().UnixNano())
		stop := h.spawn(t, pattern, "normal")
		vanish := h.spawn(t, pattern, "normal")
		changed := h.spawn(t, pattern, "normal")
		resist := h.spawn(t, pattern, "resist")
		id := h.send(t, chat.FullAuto, commandRequest("pkill", "-f", pattern))
		a := h.pending(t, id, 0)
		assertTargets(t, a, stop, vanish, changed, resist)
		late := h.spawn(t, pattern, "normal")
		fmt.Fprintln(vanish.in, "exit")
		vanish.exited(t)
		fmt.Fprintln(changed.in, "change")
		changed.ready(t)
		h.decide(t, id, a, true)
		force := h.pending(t, id, 1)
		assertTargets(t, force, resist)
		if !force.Command.Force || force.ID == a.ID {
			t.Fatal("missing distinct force authorization")
		}
		s, e := h.c.Read(ctx, id, 0)
		if e != nil {
			t.Fatal(e)
		}
		first := s.Actions[0]
		if len(first.Command.Stopped) != 1 || len(first.Command.Vanished) != 1 || len(first.Command.Skipped) != 1 || len(first.Command.Survivors) != 1 || first.Result != "Stopped 1, vanished 1, skipped 1, survivors 1" {
			t.Fatalf("all categories: %+v", first)
		}
		for _, group := range [][]chat.ProcessRow{first.Command.Stopped, first.Command.Vanished, first.Command.Skipped, first.Command.Survivors} {
			if group[0].Reason == "" {
				t.Fatal("missing per-target result reason")
			}
		}
		for _, tc := range []struct {
			rows  []chat.ProcessRow
			child *commandChild
		}{{first.Command.Stopped, stop}, {first.Command.Vanished, vanish}, {first.Command.Skipped, changed}, {first.Command.Survivors, resist}} {
			if tc.rows[0].PID != int32(tc.child.cmd.Process.Pid) {
				t.Fatalf("wrong outcome identity: %+v", tc.rows)
			}
		}
		resist.alive(t)
		changed.alive(t)
		late.alive(t)
		h.decide(t, id, force, true)
		s = h.idle(t, id)
		stop.exited(t)
		resist.exited(t)
		changed.alive(t)
		late.alive(t)
		if len(s.Actions[1].Command.Stopped) != 1 || s.Actions[1].Command.Stopped[0].PID != int32(resist.cmd.Process.Pid) {
			t.Fatal("force widened snapshot")
		}
	})
	t.Run("deny-and-stop", func(t *testing.T) {
		for _, kind := range []string{"deny", "stop"} {
			t.Run(kind, func(t *testing.T) {
				child := h.spawn(t, "decision-fixture", "normal")
				args := []string{"kill", fmt.Sprint(child.cmd.Process.Pid)}
				if runtime.GOOS == "windows" {
					args = []string{"taskkill", "/PID", fmt.Sprint(child.cmd.Process.Pid)}
				}
				id := h.send(t, chat.Confirm, commandRequest(args...))
				a := h.pending(t, id, 0)
				assertTargets(t, a, child)
				if kind == "deny" {
					h.decide(t, id, a, false)
					s := h.idle(t, id)
					if s.Actions[0].Status != "denied" {
						t.Fatal(s.Actions)
					}
				} else {
					if e := h.c.Stop(ctx, id); e != nil {
						t.Fatal(e)
					}
					h.until(t, id, func(s chat.State) bool { return !s.Active })
					if e := h.c.Decide(ctx, id, a.ID, true); e == nil {
						t.Fatal("stopped card allowed")
					}
				}
				child.alive(t)
			})
		}
	})
	t.Run("automatic-read", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows validated-read executable grammar is covered by native unit tests")
		}
		for _, mode := range []chat.Mode{chat.Auto, chat.FullAuto} {
			id := h.send(t, mode, commandRequest("cat", "read.txt"))
			s := h.idle(t, id)
			if len(s.Actions) != 1 || s.Actions[0].Status != "done" || s.Actions[0].DecidedBy != string(mode) || s.Actions[0].Command.Class != commandpolicy.Read {
				t.Fatalf("auto read: %+v", s.Actions)
			}
		}
	})
	t.Run("stop-running-command", func(t *testing.T) {
		if cap := commandpolicy.ProbeSandbox(ctx); !cap.Available {
			t.Skipf("native sandbox required for running Other: %s", cap.Reason)
		}
		// The broker owns and reaps this fixture; the test never signals it.
		runner := filepath.Join(project, "running-fixture")
		b, e := os.ReadFile(helper)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(runner, b, 0700); e != nil {
			t.Fatal(e)
		}
		marker := filepath.Join(project, "running-ready")
		id := h.send(t, chat.Confirm, commandRequest(runner, "run", marker))
		t.Cleanup(func() { _ = h.c.Stop(ctx, id) })
		a := h.pending(t, id, 0)
		h.decide(t, id, a, true)
		waitCommandCondition(t, ctx, func() bool { b, e := os.ReadFile(marker); return e == nil && len(b) > 0 })
		if e = h.c.Stop(ctx, id); e != nil {
			t.Fatal(e)
		}
		waitCommandCondition(t, ctx, func() bool {
			s, e := h.c.Read(ctx, id, 0)
			return e == nil && !s.Active && len(s.Actions) == 1 && s.Actions[0].Status == "failed" && strings.Contains(s.Actions[0].Result, "context canceled")
		})
	})
	t.Run("full-auto-destructive-or-Windows-refusal", func(t *testing.T) {
		id := h.send(t, chat.FullAuto, commandRequest("rm", "read.txt"))
		if runtime.GOOS == "windows" {
			s := h.idle(t, id)
			if len(s.Actions) != 1 || s.Actions[0].Command.Error != "Running commands needs a sandbox, which Windows does not have yet." || s.Actions[0].Status != "denied" {
				t.Fatal(s.Actions)
			}
			return
		}
		cap := commandpolicy.ProbeSandbox(ctx)
		if !cap.Available {
			s := h.idle(t, id)
			if len(s.Actions) != 1 || s.Actions[0].Status != "denied" {
				t.Fatal(s.Actions)
			}
			return
		}
		a := h.pending(t, id, 0)
		if a.Command.Class != commandpolicy.Destructive || a.GrantEligible {
			t.Fatal(a)
		}
		h.decide(t, id, a, false)
		h.idle(t, id)
		if _, e := os.Stat(filepath.Join(project, "read.txt")); e != nil {
			t.Fatal("destructive command ran before Allow", e)
		}
	})
	t.Run("Windows-literal-process-and-force", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("native Windows case")
		}
		child := h.spawn(t, "windows-literal", "normal")
		id := h.send(t, chat.FullAuto, commandRequest("taskkill", "/PID", fmt.Sprint(child.cmd.Process.Pid)))
		a := h.pending(t, id, 0)
		assertTargets(t, a, child)
		h.decide(t, id, a, true)
		force := h.pending(t, id, 1)
		assertTargets(t, force, child)
		if !force.Command.Force {
			t.Fatal(force)
		}
		child.alive(t)
		h.decide(t, id, force, true)
		s := h.idle(t, id)
		child.exited(t)
		if len(s.Actions[1].Command.Stopped) != 1 {
			t.Fatal(s.Actions)
		}
	})
	t.Run("one-shot-request-replay", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("cat fixture is Unix")
		}
		id := h.send(t, chat.Auto, commandRequest("cat", "read.txt"), commandStep{Replay: true, Input: commandpolicy.Request{Argv: []string{"cat", "read.txt"}}})
		s := h.idle(t, id)
		if len(s.Actions) != 2 {
			t.Fatalf("replay created another action: %+v", s.Actions)
		}
	})
	t.Run("grants-revoke-and-delegated-allow", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("cat grant fixture is Unix")
		}
		arrived := make(chan struct{}, 1)
		release := make(chan struct{})
		barrier := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			arrived <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
			}
			w.WriteHeader(200)
		}))
		defer barrier.Close()
		defer close(release)
		p := chat.Prompt{ID: "delegated-fixture", Session: "other-agent", Cwd: project, Tool: "Bash", Input: json.RawMessage(`{"command":"printf delegated-exact-input"}`)}
		answer := make(chan chat.PromptDecision, 1)
		go func() { v, _ := h.c.WaitPrompt(ctx, p); answer <- v }()
		waitCommandCondition(t, ctx, func() bool {
			ps, _ := h.c.Prompts(ctx)
			return slices.ContainsFunc(ps, func(v chat.Prompt) bool { return v.ID == p.ID })
		})
		id := h.send(t, chat.Confirm, commandRequest("cat", "read.txt"), commandRequest("cat", "read.txt"), commandStep{Barrier: barrier.URL}, commandStep{Tool: "answer_prompt", Input: map[string]any{"id": p.ID, "allow": true}}, commandStep{Barrier: barrier.URL}, commandRequest("cat", "read.txt"))
		a := h.pending(t, id, 0)
		if !a.GrantEligible {
			t.Fatal("missing conversation grant offer")
		}
		if e := h.c.DecideSimilar(ctx, id, a.ID); e != nil {
			t.Fatal(e)
		}
		select {
		case <-arrived:
		case <-time.After(30 * time.Second):
			t.Fatal("grant failed to auto-allow second read")
		}
		s, e := h.c.Read(ctx, id, 0)
		if e != nil {
			t.Fatal(e)
		}
		if len(s.Actions) != 2 || s.Actions[1].DecidedBy != "allowed by your grant" || s.Actions[1].GrantID == "" {
			t.Fatal(s.Actions)
		}
		g := s.Actions[1].GrantID
		// Keep the grant active in this same conversation. Full auto still
		// cannot consume it to answer another agent's prompt.
		if e := h.c.SetMode(ctx, id, chat.FullAuto); e != nil {
			t.Fatal(e)
		}
		release <- struct{}{}
		card := h.pending(t, id, 2)
		if card.Delegated == nil || string(card.Delegated.Input) != string(p.Input) || card.GrantEligible || card.GrantID != "" {
			t.Fatal(card)
		}
		select {
		case <-answer:
			t.Fatal("delegated allow escaped card")
		default:
		}
		h.decide(t, id, card, true)
		select {
		case v := <-answer:
			if v.Behavior != "allow" {
				t.Fatal(v)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("person's delegated allow not delivered")
		}
		// Release the third read after revocation and observe a fresh card.
		select {
		case <-arrived:
		case <-time.After(30 * time.Second):
			t.Fatal("delegated completion barrier missing")
		}
		if e := h.c.RevokeGrant(ctx, g); e != nil {
			t.Fatal(e)
		}
		if e := h.c.SetMode(ctx, id, chat.Confirm); e != nil {
			t.Fatal(e)
		}
		release <- struct{}{}
		third := h.pending(t, id, 3)
		h.decide(t, id, third, false)
		h.idle(t, id)
		history, e := h.c.GrantHistory(ctx)
		if e != nil || !slices.ContainsFunc(history, func(d chat.GrantDecision) bool { return d.GrantID == g && d.Revoked }) {
			t.Fatal(history, e)
		}
	})
	t.Run("prompt-burst-frozen-members", func(t *testing.T) {
		answers := make(chan chat.PromptDecision, 3)
		start := func(id string) {
			go func() {
				v, _ := h.c.WaitPrompt(ctx, chat.Prompt{ID: id, Session: "burst-agent", Cwd: project, Tool: "Write", Input: json.RawMessage(`{"file_path":"fixture.txt","content":"visible"}`)})
				answers <- v
			}()
		}
		start("burst-first")
		start("burst-second")
		var burst chat.PromptBurst
		waitCommandCondition(t, ctx, func() bool {
			gs, _ := h.c.PromptBursts(ctx)
			for _, g := range gs {
				if g.Session == "burst-agent" && len(g.Members) == 2 {
					burst = g
					return true
				}
			}
			return false
		})
		members := []string{burst.Members[0].ID, burst.Members[1].ID}
		for _, m := range burst.Members {
			if m.InputHash == "" || len(m.Prompt.Input) == 0 {
				t.Fatal("hidden burst input")
			}
		}
		start("burst-later")
		waitCommandCondition(t, ctx, func() bool {
			ps, _ := h.c.Prompts(ctx)
			return slices.ContainsFunc(ps, func(p chat.Prompt) bool { return p.ID == "burst-later" })
		})
		if e := h.c.DecideBurst(ctx, burst.ID, members, true); e != nil {
			t.Fatal(e)
		}
		for range 2 {
			select {
			case v := <-answers:
				if v.Behavior != "allow" {
					t.Fatal(v)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("burst answer missing")
			}
		}
		ps, e := h.c.Prompts(ctx)
		if e != nil || !slices.ContainsFunc(ps, func(p chat.Prompt) bool { return p.ID == "burst-later" }) {
			t.Fatal("late member was approved", ps, e)
		}
		if e := h.c.AnswerPrompt(ctx, "burst-later", false); e != nil {
			t.Fatal(e)
		}
		select {
		case v := <-answers:
			if v.Behavior != "deny" {
				t.Fatal(v)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("individual deny missing")
		}
		if e := h.c.DecideBurst(ctx, burst.ID, members, true); e == nil {
			t.Fatal("burst replay allowed")
		}
	})
}

func waitCommandCondition(t *testing.T, ctx context.Context, ready func() bool) {
	t.Helper()
	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for !ready() {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-timeout.C:
			t.Fatal("fixture condition timeout")
		case <-tick.C:
		}
	}
}
