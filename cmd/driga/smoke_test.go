package main

// Smoke test: build the real binary and drive it the way a user would, with
// every Driga directory and agent HOME isolated in temp dirs. Process and
// machine readings come from the real OS, read-only. Skipped with -short.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/cli"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/store"
)

// logBuffer is written by a child process's copier while the test reads it.
type logBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *logBuffer) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

type node struct {
	t    *testing.T
	bin  string
	root string
}

func newNode(t *testing.T, bin, name string) node {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	for _, dir := range []string{"data", "spool", "home"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return node{t, bin, root}
}

func (n node) path(parts ...string) string {
	return filepath.Join(append([]string{n.root}, parts...)...)
}

func (n node) cmd(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, n.bin, args...)
	home := n.path("home")
	cmd.Env = append(os.Environ(),
		"HOME="+home, "USERPROFILE="+home, "APPDATA="+filepath.Join(home, "AppData", "Roaming"), "LOCALAPPDATA="+filepath.Join(home, "AppData", "Local"), "CLAUDE_CONFIG_DIR=", "CODEX_HOME="+filepath.Join(home, ".codex"), "COPILOT_HOME="+filepath.Join(home, ".copilot"), "GEMINI_CLI_HOME=",
		// Linux service logs and trash resolve through XDG; keep them isolated too.
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"), "XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"DRIGA_DATA_DIR="+n.path("data"), "DRIGA_SPOOL_DIR="+n.path("spool"), "DRIGA_CONFIG="+n.path("config.toml"),
		// System tools only: the binary must never reach a real agent CLI, tmux or notifier.
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin")
	return cmd
}

// try runs a command and returns stdout, stderr and the exit error.
func (n node) try(args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var out, errout bytes.Buffer
	cmd := n.cmd(ctx, args...)
	cmd.Stdout, cmd.Stderr = &out, &errout
	err := cmd.Run()
	return out.String(), errout.String(), err
}

// run fails the test unless the command exits 0, and returns stdout.
func (n node) run(args ...string) string {
	n.t.Helper()
	out, errout, err := n.try(args...)
	if err != nil {
		n.t.Fatalf("driga %s: %v\nstdout: %s\nstderr: %s", strings.Join(args, " "), err, out, errout)
	}
	return out
}

// data returns a --json envelope's data field, re-encoded compactly.
func (n node) data(args ...string) string {
	n.t.Helper()
	var envelope struct {
		Schema int             `json:"schema"`
		Data   json.RawMessage `json:"data"`
	}
	out := n.run(append(args, "--json")...)
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || envelope.Schema != 2 {
		n.t.Fatalf("driga %v: bad envelope %v: %s", args, err, out)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, envelope.Data); err != nil {
		n.t.Fatal(err)
	}
	return compact.String()
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Smoke", "-c", "user.email=smoke@example.test"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// field reports whether compact JSON holds key with a string value containing want.
func field(data, key, want string) bool {
	_, rest, ok := strings.Cut(data, `"`+key+`":"`)
	for ok {
		value, _, _ := strings.Cut(rest, `"`)
		if strings.Contains(value, want) {
			return true
		}
		_, rest, ok = strings.Cut(rest, `"`+key+`":"`)
	}
	return false
}

func TestSmoke(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix path or action contract; Windows phase 1 has dedicated watch/refusal tests")
	}

	if testing.Short() {
		t.Skip("builds and runs the driga binary")
	}
	bin := filepath.Join(t.TempDir(), "driga")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}

	local := newNode(t, bin, "local")
	// One project with a linked worktree, and one Claude session inside it.
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo = filepath.Join(repo, "smoke-project")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-b", "main")
	git(t, repo, "commit", "--allow-empty", "-m", "first subject")
	git(t, repo, "worktree", "add", "-b", "topic", filepath.Join(filepath.Dir(repo), "smoke-topic"))
	now := time.Now().UTC()
	ts := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	transcript := `{"type":"user","sessionId":"smoke-session","cwd":"` + repo + `","gitBranch":"main","timestamp":"` + ts(-2*time.Minute) + `","message":{"content":"Update the example"}}
{"type":"ai-title","sessionId":"smoke-session","timestamp":"` + ts(-2*time.Minute) + `","aiTitle":"Smoke title"}
{"type":"assistant","sessionId":"smoke-session","timestamp":"` + ts(-time.Minute) + `","message":{"content":[],"stop_reason":"end_turn"}}
`
	write(t, local.path("home", ".claude", "projects", "-smoke-project", "smoke-session.jsonl"), transcript)
	write(t, local.path("config.toml"), "roots = ['"+filepath.Dir(repo)+"']\n[fleet]\nserve = false\n[intake]\notlp = false\n[adapters.copilot]\nenabled = false\n")

	if v := local.run("version"); strings.TrimSpace(v) == "" {
		t.Fatal("empty version")
	}
	local.run("config", "show")
	local.run("service", "run", "--once")
	// Subsequent reads use this real service's database instead of collecting
	// the host again for every command. Fleet exercises this same service below.
	write(t, local.path("config.toml"), "roots = ['"+filepath.Dir(repo)+"']\n[fleet]\nserve = true\nlisten = \"127.0.0.1:0\"\n[intake]\notlp = false\n[adapters.copilot]\nenabled = false\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var serveLog logBuffer
	serve := local.cmd(ctx, "service", "run", "--serve")
	serve.Stdout, serve.Stderr = &serveLog, &serveLog
	if err := serve.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = serve.Wait() }()
	eventually(t, 30*time.Second, func() bool {
		probeCtx, stop := context.WithTimeout(ctx, 2*time.Second)
		defer stop()
		return local.cmd(probeCtx, "fleet", "status").Run() == nil
	}, serveLog.String)

	if agents := local.data("agents", "--all"); !field(agents, "title", "Smoke title") {
		t.Fatalf("session missing from agents: %s", agents)
	}
	if projects := local.data("projects"); !field(projects, "name", "smoke-project") {
		t.Fatalf("project missing: %s", projects)
	}
	local.run("inbox")
	local.run("orphans")
	local.run("machines", "show")
	local.run("doctor")
	// Exercise the real CLI cleanup handler against the collected fixture and
	// actual Git worktree, with deterministic process inspection. Other agents
	// running tests on this host must neither delay this check nor make it skip.
	t.Run("cleanup", func(t *testing.T) {
		fixture := cleanupNode(t, local, repo)
		for key, value := range map[string]string{
			"HOME": fixture.path("home"), "PATH": "/usr/bin:/bin:/usr/sbin:/sbin",
			"DRIGA_DATA_DIR": fixture.path("data"), "DRIGA_SPOOL_DIR": fixture.path("spool"),
			"DRIGA_CONFIG": fixture.path("config.toml"),
		} {
			t.Setenv(key, value)
		}
		for _, why := range []bool{false, true} {
			var out bytes.Buffer
			g := cli.Globals{JSON: true, Stdout: &out, Stderr: &out, CleanupPlatform: &platform.Fake{}}
			if err := (cli.CleanupListCmd{Why: why}).Run(&g); err != nil {
				t.Fatalf("cleanup why=%t: %v", why, err)
			}
			var envelope struct {
				Schema int                 `json:"schema"`
				Data   []cleanup.Candidate `json:"data"`
			}
			if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || envelope.Schema != 2 {
				t.Fatalf("cleanup envelope: %v: %s", err, out.String())
			}
			found := false
			for _, candidate := range envelope.Data {
				if candidate.Path != filepath.Join(filepath.Dir(repo), "smoke-topic") {
					continue
				}
				found = true
				if !why {
					t.Fatalf("unidle worktree offered: %+v", candidate)
				}
				if candidate.Status != "refused" {
					t.Fatalf("--why worktree not refused: %+v", candidate)
				}
			}
			if why && !found {
				t.Fatalf("--why lacks refused worktree: %s", out.String())
			}
		}
	})
	// The agent count includes whatever agents the host is running, so only
	// the line's fixed parts are checked.
	summary := local.run()
	for _, part := range []string{" agents (", " working, ", "inbox ", "orphans ", "daemon"} {
		if !strings.Contains(summary, part) {
			t.Fatalf("summary lacks %q: %s", part, summary)
		}
	}
	// Keep one explicit snapshot read through the built binary. The fixture's
	// adapter roots are isolated, and no assertion depends on the host's counts.
	if snapshot := local.run("summary", "--snapshot"); !strings.Contains(snapshot, "snapshot") {
		t.Fatalf("explicit snapshot mode missing: %s", snapshot)
	}

	// Fleet: the local node serves on loopback; a viewer with an empty HOME
	// pairs and must see the served session through the stream.
	addrBytes, err := os.ReadFile(local.path("data", "fleet", "listen"))
	if err != nil {
		t.Fatal(err)
	}
	addr := strings.TrimSpace(string(addrBytes))
	peerID := strings.TrimSpace(local.run("fleet", "id"))

	viewer := newNode(t, bin, "viewer")
	write(t, viewer.path("config.toml"), "[adapters.claude]\nenabled = false\n[adapters.codex]\nenabled = false\n[fleet]\nserve = false\n[intake]\notlp = false\n[adapters.copilot]\nenabled = false\n")
	viewerID := strings.TrimSpace(viewer.run("fleet", "id"))
	local.run("fleet", "enroll", "--allow-viewer", viewerID)
	viewer.run("fleet", "peers", "add", addr, "--name", "smoke-peer", "--device-id", peerID)
	var viewerLog logBuffer
	pull := viewer.cmd(ctx, "service", "run")
	pull.Stdout, pull.Stderr = &viewerLog, &viewerLog
	if err := pull.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = pull.Wait() }()
	eventually(t, 60*time.Second, func() bool {
		_ = os.WriteFile(viewer.path("data", "viewer.heartbeat"), nil, 0600)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := viewer.cmd(ctx, "agents", "--all", "--json").Output()
		return err == nil && bytes.Contains(out, []byte("Smoke title"))
	}, func() string { return "serve:\n" + serveLog.String() + "\nviewer:\n" + viewerLog.String() })
	if machines := viewer.data("machines"); !field(machines, "name", "smoke-peer") {
		t.Fatalf("peer missing from machines: %s", machines)
	}
}

// Retain the collector's actual fixture attribution while excluding repositories
// discovered from unrelated host processes. Cleanup must inspect only test trees.
func cleanupNode(t *testing.T, source node, repo string) node {
	t.Helper()
	fixture := newNode(t, source.bin, "cleanup")
	write(t, fixture.path("config.toml"), "")
	collected, err := store.Open(source.path("data", "driga.db"), store.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer collected.Close()
	isolated, err := store.Open(fixture.path("data", "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer isolated.Close()
	err = collected.Tx(context.Background(), func(from *store.Tx) error {
		repos, err := from.Repos()
		if err != nil {
			return err
		}
		for _, r := range repos {
			if r.Path != repo {
				continue
			}
			worktrees, err := from.Worktrees(r.ID)
			if err != nil {
				return err
			}
			if len(worktrees) != 2 {
				return fmt.Errorf("fixture collector returned %d worktrees, want 2", len(worktrees))
			}
			sessions, err := from.Sessions(store.SessionFilter{IncludeEnded: true})
			if err != nil {
				return err
			}
			return isolated.Tx(context.Background(), func(to *store.Tx) error {
				repoID, err := to.UpsertRepo(r)
				if err != nil {
					return err
				}
				worktreeIDs := map[int64]int64{}
				for _, w := range worktrees {
					if w.Path != repo && w.Path != filepath.Join(filepath.Dir(repo), "smoke-topic") {
						return fmt.Errorf("unexpected fixture worktree %q", w.Path)
					}
					w.RepoID = repoID
					id, err := to.UpsertWorktree(w)
					if err != nil {
						return err
					}
					worktreeIDs[w.ID] = id
				}
				for _, s := range sessions {
					if s.ExternalID != "smoke-session" || s.Cwd != repo {
						continue
					}
					s.RepoID = &repoID
					if s.WorktreeID != nil {
						id, ok := worktreeIDs[*s.WorktreeID]
						if !ok {
							return fmt.Errorf("fixture session references uncollected worktree")
						}
						s.WorktreeID = &id
					}
					_, err := to.UpsertSession(s)
					return err
				}
				return fmt.Errorf("fixture session not collected")
			})
		}
		return fmt.Errorf("fixture repository not collected")
	})
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func eventually(t *testing.T, limit time.Duration, ok func() bool, logs func() string) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %s\n%s", limit, logs())
		}
		time.Sleep(500 * time.Millisecond)
	}
}
