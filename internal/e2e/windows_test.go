//go:build windows && e2e

// Package e2e drives the built CLI against disposable homes and owned processes.
package e2e

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/fixturegen"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/pathx"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
	"golang.org/x/sys/windows"
)

type suite struct{ bin, helper, source string }
type fixture struct {
	suite
	home, repo, data, spool string
}

func TestWindowsEndToEnd(t *testing.T) {
	source, err := filepath.Abs(filepath.Join("..", ".."))
	must(t, err)
	buildDir := t.TempDir()
	s := suite{bin: filepath.Join(buildDir, "driga.exe"), helper: filepath.Join(buildDir, "claude.exe"), source: source}
	for _, build := range []struct{ out, pkg string }{{s.bin, "./cmd/driga"}, {s.helper, "./internal/e2e/testdata/agent"}} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		cmd := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-o", build.out, build.pkg)
		cmd.Dir = source
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("build %s: %v\n%s", build.pkg, err, out)
		}
	}
	t.Run("Snapshot attribution", func(t *testing.T) {
		f := s.isolate(t)
		agent := f.agent(t)
		f.assertSession(t, "snapshot", agent.cmd.Process.Pid, "--snapshot")
	})
	t.Run("Foreground collector", func(t *testing.T) {
		f := s.isolate(t)
		agent := f.agent(t)
		collector := f.start(t, f.bin, "collect")
		var last []byte
		t.Cleanup(func() {
			if t.Failed() {
				agents, _ := f.try("", "agents", "--json")
				t.Logf("last service status:\n%s\nagents:\n%s", last, agents)
			}
		})
		eventually(t, "foreground collector lock and attributed stored session", func() bool {
			out, err := f.try("", "service", "status")
			last = out
			return err == nil && strings.Contains(string(out), "lock held=true") && f.hasSession("daemon")
		})
		f.assertSession(t, "daemon", agent.cmd.Process.Pid)
		f.stopConsole(t, collector)
		if out := f.run(t, "", "service", "status"); strings.Contains(string(out), "lock held=true") {
			t.Fatalf("collector lock survived clean stop: %s", out)
		}
	})
	t.Run("Service lifecycle", func(t *testing.T) {
		f := s.isolate(t)
		agent := f.agent(t)
		// Refuse an existing per-user task: cleanup must never stop somebody else's collector.
		before := f.run(t, "", "service", "status")
		if !strings.Contains(string(before), "task: not installed") {
			t.Fatalf("requires unused per-user scheduled task; got %s", before)
		}
		// Install starts the task; start can fail after successful registration.
		installed := false
		t.Cleanup(func() {
			if !installed {
				return
			}
			status, err := f.try("", "service", "status")
			if err == nil && strings.Contains(string(status), "task: not installed") {
				return
			}
			out, err := f.try("", "service", "uninstall")
			if err != nil {
				t.Errorf("uninstall owned task: %v %s", err, out)
			}
		})
		installOut, installErr := f.try("", "service", "install")
		installed = installErr == nil || strings.Contains(installErr.Error(), "installed collector; start with")
		if installErr != nil {
			t.Fatalf("install scheduled collector: %v %s", installErr, installOut)
		}
		running := func() {
			eventually(t, "scheduled task running with isolated collector lock", func() bool {
				out, err := f.try("", "service", "status")
				return err == nil && strings.Contains(string(out), "task: running") && strings.Contains(string(out), "lock held=true") && f.hasSession("daemon")
			})
		}
		running()
		f.assertSession(t, "daemon", agent.cmd.Process.Pid)
		// The task must record the isolation; Scheduler does not inherit our environment.
		token, err := windows.GetCurrentProcessToken().GetTokenUser()
		must(t, err)
		task := `\Driga\` + token.User.Sid.String() + `\Driga collector`
		out := f.external(t, "", "schtasks.exe", "/Query", "/TN", task, "/XML")
		// schtasks emits UTF-16 on some Windows versions. Decode before checking paths.
		text := decodeSchedulerXML(out)
		for _, value := range []string{"--service-env", f.home, f.data, f.spool, os.Getenv("DRIGA_CONFIG")} {
			if !strings.Contains(text, value) {
				t.Fatalf("scheduled task lost isolation %q:\n%s", value, text)
			}
		}
		f.run(t, "", "service", "stop")
		if out := f.run(t, "", "service", "status"); !strings.Contains(string(out), "task: ready") || strings.Contains(string(out), "lock held=true") {
			t.Fatalf("stop did not become ready: %s", out)
		}
		f.run(t, "", "service", "start")
		running()
		f.run(t, "", "service", "uninstall")
		if out := f.run(t, "", "service", "status"); !strings.Contains(string(out), "task: not installed") {
			t.Fatalf("task survived uninstall: %s", out)
		}
		// Cleanup is now idempotent through status, rather than uninstalling a missing task.
	})
	t.Run("Web", func(t *testing.T) {
		f := s.isolate(t)
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		must(t, err)
		address := listener.Addr().String()
		must(t, listener.Close())
		web := f.start(t, f.bin, "web", "--listen", address)
		jar, err := cookiejar.New(nil)
		must(t, err)
		client := &http.Client{Timeout: 2 * time.Second, Jar: jar}
		var dashboardURL string
		eventually(t, "dashboard prints authenticated loopback URL", func() bool {
			log, err := os.ReadFile(web.log)
			if err != nil {
				return false
			}
			for _, word := range strings.Fields(string(log)) {
				if strings.HasPrefix(word, "http://"+address+"/") {
					dashboardURL = word
					return true
				}
			}
			return false
		})
		eventually(t, "loopback dashboard ready", func() bool {
			r, err := client.Get(dashboardURL)
			if err != nil {
				return false
			}
			defer r.Body.Close()
			return r.StatusCode == http.StatusOK
		})
		for _, path := range []string{"/", "/static/brand/favicon.svg"} {
			r, err := client.Get("http://" + address + path)
			must(t, err)
			body, err := io.ReadAll(r.Body)
			r.Body.Close()
			must(t, err)
			if r.StatusCode != 200 || len(body) == 0 {
				t.Fatalf("GET %s: status=%d bytes=%d", path, r.StatusCode, len(body))
			}
			if csp := r.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
				t.Fatalf("GET %s: missing restrictive CSP: %q", path, csp)
			}
		}
		f.stopConsole(t, web)
	})
	t.Run("Hooks", func(t *testing.T) {
		f := s.isolate(t)
		_ = f.agent(t)
		settings := filepath.Join(f.home, ".claude", "settings.json")
		original := []byte("{\n  \"permissions\": {\"allow\": [\"Read\"]}\n}\n")
		must(t, os.WriteFile(settings, original, 0600))
		diff := f.run(t, "", "hooks", "install", "claude", "--dry-run")
		if !strings.Contains(string(diff), "SessionStart") {
			t.Fatalf("missing hook preview: %s", diff)
		}
		unchanged, err := os.ReadFile(settings)
		must(t, err)
		if !bytes.Equal(unchanged, original) {
			t.Fatal("hook dry-run changed settings")
		}
		f.run(t, "", "hooks", "install", "claude", "--yes")
		installed, err := os.ReadFile(settings)
		must(t, err)
		var cfg struct {
			Hooks map[string][]struct {
				Hooks []struct {
					Command string
					Args    []string
				}
			}
		}
		must(t, json.Unmarshal(installed, &cfg))
		groups := cfg.Hooks["Notification"]
		if len(groups) != 1 || len(groups[0].Hooks) != 1 {
			t.Fatalf("Notification hook missing: %s", installed)
		}
		hook := groups[0].Hooks[0]
		if hook.Command != f.bin || !slices.Equal(hook.Args, []string{"hook", "claude", "Notification"}) {
			t.Fatalf("unexpected executable/args hook: %+v", hook)
		}
		// Claude's Windows executable/args form is passed through cmd.exe /c here;
		// use the installed values verbatim, with cmd's required outer quote pair.
		words := []string{`"` + hook.Command + `"`}
		for _, arg := range hook.Args {
			words = append(words, syscall.EscapeArg(arg))
		}
		commandLine := `"` + strings.Join(words, " ") + `"`
		payload, _ := json.Marshal(map[string]string{"session_id": "windows-e2e", "cwd": f.repo, "hook_event_name": "Notification", "notification_type": "permission_prompt", "tool_name": "Bash"})
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		cmd := exec.CommandContext(ctx, "cmd.exe")
		// cmd.exe has different quoting rules from CommandLineToArgvW. Supply its
		// command line directly so Go does not backslash-escape the shell quotes.
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /d /s /c ` + commandLine}
		cmd.Dir = f.home
		cmd.Stdin = bytes.NewReader(payload)
		hookOut, hookErr := cmd.CombinedOutput()
		cancel()
		if hookErr != nil {
			t.Fatalf("installed Claude hook via cmd.exe: %v %s", hookErr, hookOut)
		}
		events, _, err := spool.Drain(f.spool, nil)
		must(t, err)
		if len(events) != 1 || events[0].SessionID != "windows-e2e" || events[0].Event != "Notification" || !pathx.Equal(events[0].Cwd, f.repo) {
			t.Fatalf("installed hook did not reach isolated spool: %+v", events)
		}
		f.run(t, "", "collect", "--once")
		st, err := store.Open(filepath.Join(f.data, "driga.db"), store.ReadOnly)
		must(t, err)
		must(t, st.Tx(context.Background(), func(tx *store.Tx) error {
			events, err := tx.Events("claude", "windows-e2e", time.Time{})
			if err != nil {
				return err
			}
			for _, event := range events {
				if event.Kind == "Notification" {
					return nil
				}
			}
			return fmt.Errorf("installed hook event was not ingested into the isolated store")
		}))
		must(t, st.Close())
		f.assertSession(t, "snapshot", 0)
		f.run(t, "", "hooks", "uninstall", "claude", "--yes")
		restored, err := os.ReadFile(settings)
		must(t, err)
		var before, after any
		must(t, json.Unmarshal(original, &before))
		must(t, json.Unmarshal(restored, &after))
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("settings not restored: %s", restored)
		}
	})
	t.Run("Kill", func(t *testing.T) {
		f := s.isolate(t)
		// No registry is written for this helper, so no live session protects it.
		work := filepath.Join(f.home, "orphan-worktree")
		f.git(t, f.repo, "worktree", "add", "-b", "orphan-test", work)
		handshake := filepath.Join(f.home, "owned-tree.json")
		t.Setenv("E2E_AGENT_MODE", "supervisor")
		t.Setenv("E2E_AGENT_HANDSHAKE", handshake)
		supervisor := f.startAt(t, work, f.helper, "mcp", "serve")
		eventually(t, "owned tree handshake", func() bool {
			b, err := os.ReadFile(handshake)
			var ids []int
			return err == nil && json.Unmarshal(b, &ids) == nil && len(ids) == 2
		})
		select {
		case err := <-supervisor.done:
			must(t, err)
		case <-time.After(10 * time.Second):
			t.Fatal("supervisor did not exit")
		}
		b, err := os.ReadFile(handshake)
		must(t, err)
		var ids []int
		must(t, json.Unmarshal(b, &ids))
		// Hold handles through verification/cleanup; never reopen a PID to terminate it.
		var handles []windows.Handle
		for _, pid := range ids {
			h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
			must(t, err)
			handles = append(handles, h)
			t.Cleanup(func() { windows.CloseHandle(h) })
		}
		t.Setenv("E2E_AGENT_MODE", "leaf")
		sentinel := f.start(t, f.helper, "sentinel")
		var candidate model.Orphan
		var last []byte
		t.Cleanup(func() {
			if t.Failed() {
				t.Logf("owned tree %v; last orphans output:\n%s", ids, last)
			}
		})
		eventually(t, "real orphan proof for exactly the owned tree", func() bool {
			out, err := f.try("", "orphans", "--snapshot", "--json")
			last = append(out, []byte(fmt.Sprint(err))...)
			if err != nil {
				return false
			}
			var envelope struct{ Data struct{ Rows []model.Orphan } }
			if json.Unmarshal(out, &envelope) != nil {
				return false
			}
			for _, o := range envelope.Data.Rows {
				if o.RootPID == int32(ids[0]) {
					candidate = o
					return true
				}
			}
			return false
		})
		got := append([]int32(nil), candidate.PIDs...)
		slices.Sort(got)
		want := []int32{int32(ids[0]), int32(ids[1])}
		slices.Sort(want)
		if !slices.Equal(got, want) || candidate.Young || !pathx.Equal(candidate.Cwd, work) {
			t.Fatalf("unsafe or incorrect candidate: %+v; owned=%v", candidate, want)
		}
		_, refusal := f.try("n\n", "orphans", "kill", candidate.ID, "--snapshot", "--force")
		var exit *exec.ExitError
		if !errors.As(refusal, &exit) || exit.ExitCode() != 2 || !strings.Contains(refusal.Error(), "not applied; pass --yes to apply without asking") {
			t.Fatalf("non-terminal kill must refuse with usage status: %v", refusal)
		}
		for _, h := range handles {
			assertAlive(t, h, true)
		}
		proof := f.run(t, "", "orphans", "kill", candidate.ID, "--snapshot", "--force", "--yes")
		if !strings.Contains(string(proof), "verified idle") || !strings.Contains(string(proof), "driga orphans kill "+candidate.ID) {
			t.Fatalf("missing idle verification/evidence: %s", proof)
		}
		for _, h := range handles {
			assertAlive(t, h, false)
		}
		select {
		case err := <-sentinel.done:
			t.Fatalf("unrelated owned sentinel died: %v", err)
		default:
		}
	})
	t.Run("Cleanup", func(t *testing.T) {
		f := s.isolate(t)
		work := filepath.Join(f.home, "recycle-worktree")
		f.git(t, f.repo, "worktree", "add", "-b", "merged-e2e", work)
		// A clean checkout at main's commit is already merged. Age its index rather
		// than weakening the shipped seven-day idle threshold.
		index := strings.TrimSpace(string(f.git(t, work, "rev-parse", "--git-path", "index")))
		if !filepath.IsAbs(index) {
			index = filepath.Join(work, index)
		}
		old := time.Now().Add(-30 * 24 * time.Hour)
		must(t, os.Chtimes(index, old, old))
		f.run(t, "", "collect", "--once")
		var envelope struct{ Data []cleanup.Candidate }
		must(t, json.Unmarshal(f.run(t, "", "cleanup", "--kind", "worktree", "--why", "--json"), &envelope))
		var candidate cleanup.Candidate
		for _, c := range envelope.Data {
			if pathx.Equal(c.Path, work) {
				candidate = c
			}
		}
		if candidate.ID == "" || candidate.Status != "ready" {
			t.Fatalf("merged clean worktree not eligible: %+v", envelope.Data)
		}
		dry := f.run(t, "", "cleanup", "remove", candidate.ID, "--dry-run", "--json")
		if !bytes.Contains(dry, []byte("IFileOperation")) {
			t.Fatalf("dry-run missing recycle command: %s", dry)
		}
		if _, err := os.Stat(work); err != nil {
			t.Fatalf("dry-run removed worktree: %v", err)
		}
		applied := f.run(t, "", "cleanup", "remove", candidate.ID, "--yes", "--json")
		if !bytes.Contains(applied, []byte(`"completed"`)) {
			t.Fatalf("cleanup not completed: %s", applied)
		}
		if _, err := os.Stat(work); !os.IsNotExist(err) {
			t.Fatalf("worktree source still exists: %v", err)
		}
		assertRecycled(t, work)
		if out := f.git(t, f.repo, "worktree", "list", "--porcelain"); strings.Contains(string(out), filepath.ToSlash(work)) || strings.Contains(string(out), work) {
			t.Fatalf("git worktree metadata retained: %s", out)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", "-C", f.repo, "show-ref", "--verify", "refs/heads/merged-e2e")
		if err := cmd.Run(); err == nil {
			t.Fatal("merged branch metadata retained")
		}
		if _, err := os.Stat(f.repo); err != nil {
			t.Fatalf("main worktree touched: %v", err)
		}
	})
	t.Run("Notification status", func(t *testing.T) {
		f := s.isolate(t)
		out := f.run(t, "", "doctor", "--json")
		var checks []struct{ Name, Detail string }
		must(t, json.Unmarshal(out, &checks))
		for _, check := range checks {
			if check.Name == "notifications" && check.Detail != "" {
				t.Log(check.Detail)
				return
			}
		}
		t.Fatalf("doctor omitted notification status: %s", out)
	})
}

func (s suite) isolate(t *testing.T) fixture {
	t.Helper()
	home := t.TempDir()
	// Local sockets live under TEMP and must stay below 108 bytes; the
	// per-test TempDir is too deep, so use a short directory beside it.
	temp, err := os.MkdirTemp(os.TempDir(), "t")
	must(t, err)
	t.Cleanup(func() { os.RemoveAll(temp) })
	f := fixture{suite: s, home: home, repo: filepath.Join(home, "groves", "grove-000"), data: filepath.Join(home, "data"), spool: filepath.Join(home, "spool")}
	for key, value := range map[string]string{
		"HOME": home, "USERPROFILE": home, "APPDATA": filepath.Join(home, "AppData", "Roaming"), "LOCALAPPDATA": filepath.Join(home, "AppData", "Local"),
		"TEMP": temp, "TMP": temp, "CLAUDE_CONFIG_DIR": "", "CODEX_HOME": filepath.Join(home, ".codex"), "COPILOT_HOME": filepath.Join(home, ".copilot"), "GEMINI_CLI_HOME": filepath.Join(home, ".gemini"),
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"), "XDG_DATA_HOME": filepath.Join(home, ".local", "share"), "XDG_STATE_HOME": filepath.Join(home, ".local", "state"),
		"DRIGA_DATA_DIR": f.data, "DRIGA_SPOOL_DIR": f.spool, "DRIGA_CONFIG": filepath.Join(home, "config.toml"), "DRIGA_INTERNAL": "",
		"GIT_COMMITTER_DATE": "2020-01-01T00:00:00Z", "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": filepath.Join(home, "gitconfig"), "GIT_TERMINAL_PROMPT": "0", "GIT_OPTIONAL_LOCKS": "0",
	} {
		t.Setenv(key, value)
	}
	must(t, os.MkdirAll(os.Getenv("TEMP"), 0700))
	must(t, os.MkdirAll(f.repo, 0700))
	// Only test-owned Claude state is enabled; no agent CLI or desktop alert runs.
	config := "roots = [" + strconv.Quote(filepath.ToSlash(f.repo)) + "]\nno_service_hint = true\norphan_min_age = '0s'\n[inbox]\nalerts = 'off'\n[adapters.codex]\nenabled = false\n[adapters.copilot]\nenabled = false\n[adapters.gemini]\nenabled = false\n"
	must(t, os.WriteFile(os.Getenv("DRIGA_CONFIG"), []byte(config), 0600))
	f.git(t, f.repo, "init", "-b", "main")
	f.git(t, f.repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "--date=2020-01-01T00:00:00Z", "-m", "fixture baseline")
	// Committer time also gates cleanup eligibility.
	return f
}

func (f fixture) agent(t *testing.T) *ownedProcess {
	t.Helper()
	agent := f.startAt(t, f.repo, f.helper)
	start, err := platform.ProcessStart(int32(agent.cmd.Process.Pid))
	must(t, err)
	generated := filepath.Join(f.home, "generated")
	must(t, fixturegen.Write(generated, fixturegen.Scenario{Now: time.Now(), Repos: 1, Agents: []fixturegen.AgentSpec{{Adapter: "claude", ID: "windows-e2e", Status: "busy"}}}))
	must(t, filepath.WalkDir(filepath.Join(generated, ".claude"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(generated, path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		b = bytes.ReplaceAll(b, []byte(fixturegen.HomeToken), []byte(filepath.ToSlash(f.home)))
		if strings.HasPrefix(filepath.ToSlash(relative), ".claude/sessions/") {
			var registry map[string]any
			if err := json.Unmarshal(b, &registry); err != nil {
				return err
			}
			registry["startedAt"] = start
			b, err = json.Marshal(registry)
			if err != nil {
				return err
			}
			relative = filepath.Join(".claude", "sessions", fmt.Sprint(agent.cmd.Process.Pid)+".json")
		}
		target := filepath.Join(f.home, relative)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		return os.WriteFile(target, b, 0600)
	}))
	return agent
}

func (f fixture) try(input string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.bin, args...)
	cmd.Dir = f.home
	cmd.Stdin = strings.NewReader(input)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return out.Bytes(), fmt.Errorf("%v: %w: %s", args, err, &stderr)
	}
	return out.Bytes(), nil
}
func (f fixture) run(t *testing.T, input string, args ...string) []byte {
	t.Helper()
	out, err := f.try(input, args...)
	if err != nil {
		t.Fatalf("driga %v: %v\n%s", args, err, out)
	}
	return out
}
func (f fixture) external(t *testing.T, input, bin string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = f.home
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", bin, args, err, out)
	}
	return out
}
func (f fixture) git(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	return f.external(t, "", "git", append([]string{"-C", dir}, args...)...)
}

type ownedProcess struct {
	cmd  *exec.Cmd
	done chan error
	stop func()
	log  string
}

func (f fixture) start(t *testing.T, bin string, args ...string) *ownedProcess {
	t.Helper()
	return f.startAt(t, f.home, bin, args...)
}
func (f fixture) startAt(t *testing.T, dir, bin string, args ...string) *ownedProcess {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE}
	log, err := os.CreateTemp(f.home, "process-*.log")
	must(t, err)
	cmd.Stdout, cmd.Stderr = log, log
	stop, err := platform.StartChild(cmd)
	must(t, err)
	p := &ownedProcess{cmd: cmd, done: make(chan error, 1), stop: stop, log: log.Name()}
	go func() { p.done <- cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		if t.Failed() {
			b, _ := os.ReadFile(log.Name())
			t.Logf("owned %s %v output:\n%s", filepath.Base(bin), args, b)
		}
		stop()
		select {
		case <-p.done:
		case <-time.After(10 * time.Second):
			t.Errorf("owned process %d failed to exit; log %s", cmd.Process.Pid, log.Name())
		}
		log.Close()
	})
	return p
}
func (f fixture) stopConsole(t *testing.T, p *ownedProcess) {
	t.Helper()
	cmd := exec.Command(f.helper)
	cmd.Env = append(os.Environ(), "E2E_AGENT_MODE=stop-console", "E2E_AGENT_PID="+fmt.Sprint(p.cmd.Process.Pid))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("signal owned console: %v %s", err, out)
	}
	select {
	case err := <-p.done:
		if err != nil {
			t.Fatalf("owned CLI did not stop cleanly: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("owned CLI ignored interrupt")
	}
	p.stop()
}
func (f fixture) hasSession(mode string) bool {
	out, err := f.try("", "agents", "--json")
	if err != nil {
		return false
	}
	var e struct {
		Mode string
		Data struct {
			Rows []struct{ Adapter, Label string }
		}
	}
	if json.Unmarshal(out, &e) != nil || e.Mode != mode {
		return false
	}
	for _, row := range e.Data.Rows {
		if row.Adapter == "claude" && row.Label == "working" {
			return true
		}
	}
	return false
}
func (f fixture) assertSession(t *testing.T, mode string, pid int, flags ...string) {
	t.Helper()
	args := append([]string{"agents", "--json"}, flags...)
	out := f.run(t, "", args...)
	var e struct {
		Mode string
		Data struct {
			Rows []struct {
				ID             int64
				Adapter, Label string
			}
		}
	}
	must(t, json.Unmarshal(out, &e))
	if e.Mode != mode {
		t.Fatalf("agents mode=%q want %q: %s", e.Mode, mode, out)
	}
	for _, row := range e.Data.Rows {
		if row.Adapter != "claude" {
			continue
		}
		detail := f.run(t, "", append([]string{"agents", "show", fmt.Sprint(row.ID), "--json"}, flags...)...)
		var d struct {
			Data struct{ Session model.Session }
		}
		must(t, json.Unmarshal(detail, &d))
		session := d.Data.Session
		if session.ExternalID != "windows-e2e" {
			continue
		}
		wantLabel := "working"
		if pid == 0 {
			wantLabel = "waiting"
		}
		if session.Label != wantLabel || !pathx.Equal(session.Cwd, f.repo) || pid != 0 && session.PID != int32(pid) {
			t.Fatalf("session cwd/state/PID mismatch: %+v; want cwd=%s label=%s pid=%d", session, f.repo, wantLabel, pid)
		}
		return
	}
	t.Fatalf("fixture session missing from agents JSON: %s", out)
}
func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func assertAlive(t *testing.T, h windows.Handle, want bool) {
	t.Helper()
	var code uint32
	must(t, windows.GetExitCodeProcess(h, &code))
	if (code == 259) != want {
		t.Fatalf("owned process alive=%t want %t (exit=%d)", code == 259, want, code)
	}
}
func decodeSchedulerXML(b []byte) string {
	if len(b) >= 2 && (b[0] == 0xff && b[1] == 0xfe || b[1] == 0) {
		var words []uint16
		for i := 0; i+1 < len(b); i += 2 {
			words = append(words, binary.LittleEndian.Uint16(b[i:i+2]))
		}
		return string(utf16.Decode(words))
	}
	return string(b)
}

// $I metadata records the original absolute path; matching its $R sibling and
// its .git file proves this exact owned directory reached the Recycle Bin.
func assertRecycled(t *testing.T, path string) {
	t.Helper()
	token, err := windows.GetCurrentProcessToken().GetTokenUser()
	must(t, err)
	dir := filepath.Join(filepath.VolumeName(path)+`\`, "$Recycle.Bin", token.User.Sid.String())
	entries, err := os.ReadDir(dir)
	must(t, err)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "$I") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		must(t, err)
		if len(b) < 24 {
			continue
		}
		offset := 24
		if binary.LittleEndian.Uint64(b[:8]) == 2 {
			offset = 28
		}
		var words []uint16
		for i := offset; i+1 < len(b); i += 2 {
			w := binary.LittleEndian.Uint16(b[i : i+2])
			if w == 0 {
				break
			}
			words = append(words, w)
		}
		if !pathx.Equal(string(utf16.Decode(words)), path) {
			continue
		}
		recycled := filepath.Join(dir, "$R"+entry.Name()[2:])
		if _, err := os.Stat(filepath.Join(recycled, ".git")); err != nil {
			t.Fatalf("Recycle Bin payload missing: %v", err)
		}
		return
	}
	t.Fatalf("no Recycle Bin item records original path %s", path)
}
