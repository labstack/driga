package fixturegen

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
)

func contents(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		files[filepath.ToSlash(rel)] = b
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestGenDeterministic(t *testing.T) {
	sc := Scenario{Repos: 2, WorktreesPerRepo: 3, Seed: 782, Crashes: true, PartialLines: true, Rotations: true, LongIdle: true,
		Agents: []AgentSpec{{Adapter: "claude"}, {Adapter: "codex", Repo: 1, Worktree: 2}}}
	a, b := t.TempDir(), t.TempDir()
	if err := Write(a, sc); err != nil {
		t.Fatal(err)
	}
	if err := Write(b, sc); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(contents(t, a), contents(t, b)) {
		t.Fatal("same scenario and seed produced different bytes")
	}
	sc.Seed++
	c := t.TempDir()
	if err := Write(c, sc); err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(contents(t, a), contents(t, c)) {
		t.Fatal("seed must influence generated identities")
	}
}

// These scenario definitions are independent of expected.json. Generation never
// creates or updates labels; those are reviewed against spec §§7.1–7.3.
func heldoutScenarios() map[string]Scenario {
	now := time.Date(2026, 10, 18, 18, 0, 0, 0, time.UTC)
	small := Scenario{Now: now, Repos: 1, WorktreesPerRepo: 1, Seed: 901, Layout: "studios/cedar grove",
		Agents: []AgentSpec{{Adapter: "claude", ID: "cedar-solo", Version: "2.1.93", Status: "busy", Edit: "src/leaf.go"}}}
	many := Scenario{Now: now, Repos: 50, WorktreesPerRepo: 3, Seed: 902, Layout: "estuary/checkouts"}
	for r := 0; r < 50; r++ {
		many.Agents = append(many.Agents,
			AgentSpec{Adapter: "claude", ID: fmtID("canopy", r), Version: "2.1.94", Repo: r, Status: "busy", Edit: "src/shared.go"},
			AgentSpec{Adapter: "codex", ID: fmtID("tributary", r), Version: "0.16.91", Repo: r, Edit: "src/shared.go"},
			AgentSpec{Adapter: "codex", ID: fmtID("delta", r), Version: "0.16.91", Repo: r, Worktree: 1, Edit: "src/shared.go"},
		)
	}
	unknown := Scenario{Now: now, Repos: 2, WorktreesPerRepo: 1, Seed: 903, Layout: "orchards",
		Agents: []AgentSpec{{Adapter: "claude", ID: "future-cedar", Version: "9.8.7-preview", Status: "idle"},
			{Adapter: "codex", ID: "future-river", Version: "8.7.6-preview", Repo: 1, Hosted: true, Completed: true, ActivityAge: 2 * time.Minute}}}
	linux := Scenario{Now: now, Repos: 2, WorktreesPerRepo: 2, Seed: 904, Layout: "home/linnea/src/研究 grove",
		Agents: []AgentSpec{{Adapter: "codex", ID: "linux-stream", Version: "0.16.92", Edit: "pkg/流.go"},
			{Adapter: "claude", ID: "linux-quiet", Version: "2.1.95", Repo: 1, Worktree: 1, Status: "busy", ActivityAge: 30 * time.Minute}}}
	crash := Scenario{Now: now, Repos: 3, WorktreesPerRepo: 1, Seed: 905, Layout: "terraces", PartialLines: true, Rotations: true,
		Agents: []AgentSpec{{Adapter: "claude", ID: "crash-registry", Version: "2.1.96", Status: "busy", Crashed: true},
			{Adapter: "codex", ID: "crash-midturn", Version: "0.16.93", Repo: 1, Crashed: true},
			{Adapter: "codex", ID: "crash-completed", Version: "0.16.93", Repo: 2, Crashed: true, Completed: true},
			{Adapter: "claude", ID: "reused-registry", Version: "2.1.96", Repo: 2, PIDReuse: true, Status: "idle"}}}
	broker := Scenario{Now: now, Repos: 1, WorktreesPerRepo: 1, Seed: 906, Layout: "wetlands",
		Agents: []AgentSpec{{Adapter: "claude", ID: "living-neighbor", Version: "2.1.97", Status: "idle"}},
		Procs: []model.Proc{
			{PID: 8100, PPID: 1, StartTime: now.Add(-2 * time.Hour), Cmdline: "node /opt/heldout-tools/app-server-broker.mjs", RSS: 20 << 20},
			{PID: 8101, PPID: 8100, StartTime: now.Add(-2 * time.Hour), Cmdline: "codex app-server", RSS: 12 << 20},
			{PID: 8102, PPID: 8101, StartTime: now.Add(-time.Hour), Cmdline: "worker-runtime", RSS: 3 << 20},
			{PID: 8200, PPID: 1, StartTime: now.Add(-2 * time.Minute), Cmdline: "claude mcp serve", RSS: 1 << 20},
			{PID: 8300, PPID: 1, StartTime: now.Add(-time.Hour), Cmdline: "unrelated-indexer", RSS: 1 << 20},
		}}
	return map[string]Scenario{
		"claude/2.1.93/heldout/one-repo-one-agent":           small,
		"mixed/heldout-v1/heldout/many-repos-many-worktrees": many,
		"mixed/future-formats/heldout/untested-versions":     unknown,
		"mixed/linux-layout/heldout/linux-paths":             linux,
		"mixed/crash-formats/heldout/crash-partial-lines":    crash,
		"mixed/broker-layout/heldout/orphan-broker-tree":     broker,
	}
}

func fmtID(prefix string, n int) string {
	// Three decimal digits keep fixture names easy to inspect.
	return prefix + "-" + string([]byte{byte('0' + n/100), byte('0' + n/10%10), byte('0' + n%10)})
}

func TestGenScenarioShapes(t *testing.T) {
	t.Run("crashes-rotations-long-idle", func(t *testing.T) {
		dir := t.TempDir()
		sc := Scenario{Repos: 1, WorktreesPerRepo: 1, Seed: 910, Crashes: true, Rotations: true, PartialLines: true, LongIdle: true,
			Agents: []AgentSpec{{Adapter: "claude"}, {Adapter: "codex"}}}
		if err := Write(dir, sc); err != nil {
			t.Fatal(err)
		}
		f, err := platform.LoadFake(dir)
		if err != nil {
			t.Fatal(err)
		}
		ps, err := f.Procs(context.Background())
		if err != nil || len(ps) != 0 {
			t.Fatalf("crashes retained agent processes: %v, %v", ps, err)
		}
		files := contents(t, dir)
		var actual Scenario
		if err := json.Unmarshal(files["scenario.json"], &actual); err != nil {
			t.Fatal(err)
		}
		var mtimes map[string]time.Time
		if err := json.Unmarshal(files["mtimes.json"], &mtimes); err != nil {
			t.Fatal(err)
		}
		rotations, logs := 0, 0
		for path, b := range files {
			if strings.HasSuffix(path, ".jsonl.1") {
				rotations++
			}
			if strings.HasSuffix(path, ".jsonl") {
				logs++
				if !bytes.HasSuffix(b, []byte(`{"type":"unfinished`)) {
					t.Fatalf("%s lacks partial line", path)
				}
				if actual.Now.Sub(mtimes[path]) != 2*time.Hour {
					t.Fatalf("%s lacks long-idle timestamp: %v", path, mtimes[path])
				}
			}
		}
		if rotations != 2 || logs != 2 {
			t.Fatalf("rotation/log count = %d/%d, want 2/2", rotations, logs)
		}
	})
	for name, sc := range heldoutScenarios() {
		t.Run(filepath.Base(name), func(t *testing.T) {
			dir := t.TempDir()
			if err := Write(dir, sc); err != nil {
				t.Fatal(err)
			}
			files := contents(t, dir)
			registries, logs, worktrees := 0, 0, 0
			for path := range files {
				if strings.HasPrefix(path, ".claude/sessions/") {
					registries++
				}
				if strings.HasSuffix(path, ".jsonl") {
					logs++
				}
				if strings.HasSuffix(path, "/git.fixture") || strings.HasSuffix(path, "/git.fixture/config") {
					worktrees++
				}
			}
			claude := 0
			for _, a := range sc.Agents {
				if a.Adapter == "claude" {
					claude++
				}
			}
			if registries != claude || logs != len(sc.Agents) || worktrees != sc.Repos*sc.WorktreesPerRepo {
				t.Fatalf("registry/log/worktree counts = %d/%d/%d; want %d/%d/%d", registries, logs, worktrees, claude, len(sc.Agents), sc.Repos*sc.WorktreesPerRepo)
			}
			if _, exists := files["expected.json"]; exists {
				t.Fatal("generator must never supply its own expected labels")
			}
			if sc.PartialLines {
				for path, b := range files {
					if strings.HasSuffix(path, ".jsonl") && bytes.HasSuffix(b, []byte("\n")) {
						t.Fatalf("%s lacks partial trailing record", path)
					}
				}
			}
			// Explicit maintainer/development regeneration; normal tests only write temp dirs.
			if out := os.Getenv("DRIGA_FIXTURE_OUTPUT"); out != "" {
				if err := Write(filepath.Join(out, filepath.FromSlash(name)), sc); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestFakeIdentityAndIsolation(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 10, 18, 16, 0, 0, 0, time.UTC)
	p := model.Proc{PID: 42, StartTime: start, Cwd: "/home/fixture/space grove"}
	b, _ := json.Marshal([]model.Proc{p})
	if err := os.WriteFile(filepath.Join(dir, "procs.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "machine.json"), []byte(`{"os":"linux"}`), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := platform.LoadFake(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Alive(42, start) || f.Alive(42, start.Add(time.Second)) || f.Alive(43, start) || f.Alive(42, time.Time{}) {
		t.Fatal("Alive must match both pid and start time exactly")
	}
	ctx := context.Background()
	ps, err := f.Procs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ps[0].Cwd = "mutated"
	ps, _ = f.Procs(ctx)
	if ps[0].Cwd != p.Cwd {
		t.Fatal("Procs exposed mutable fixture table")
	}
	cwds, err := f.Cwds(ctx, []int32{42, 43})
	if err != nil || len(cwds) != 1 || cwds[42] != p.Cwd {
		t.Fatalf("Cwds = %v, %v", cwds, err)
	}
	if _, err := f.Machine(ctx); err != nil {
		t.Fatalf("optional sample: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "machine.json"), []byte(`{"os":"linux","cores":12}`), 0600); err != nil {
		t.Fatal(err)
	}
	f, err = platform.LoadFake(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, err := f.Machine(ctx)
	if err != nil || m.Cores != 12 {
		t.Fatalf("machine = %+v, %v", m, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "procs.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.LoadFake(dir); err == nil {
		t.Fatal("malformed procs accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "procs.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "machine.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.LoadFake(dir); err == nil {
		t.Fatal("malformed optional sample accepted")
	}
}
