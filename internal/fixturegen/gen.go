// Package fixturegen writes portable synthetic agent homes, never expected labels.
package fixturegen

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/driga/internal/model"
)

// HomeToken is replaced with the copied fixture's HOME before collection.
const HomeToken = "{{FIXTURE_HOME}}"

// AgentSpec defines a session independently of any derived display label.
// Repo and Worktree are zero-based. ActivityAge defaults to 20 seconds.
type AgentSpec struct {
	Adapter, ID, Version, Status, Edit   string
	Repo, Worktree                       int
	ActivityAge                          time.Duration
	Crashed, PIDReuse, Hosted, Completed bool
}

// Scenario describes a reproducible process tree, Git layout, and agent logs.
// Now defaults to 2026-10-18T18:00:00Z. Layout is a relative home directory.
// Crashes removes each agent process; Rotations preserves a .jsonl.1 predecessor;
// PartialLines adds malformed/unknown records and an incomplete final record;
// LongIdle sets activity to two hours ago unless an explicit age was supplied.
type Scenario struct {
	Agents                                     []AgentSpec
	Repos, WorktreesPerRepo                    int
	Crashes, PartialLines, Rotations, LongIdle bool
	Seed                                       int64
	Now                                        time.Time
	Layout                                     string
	Procs                                      []model.Proc
}

// Write creates a synthetic HOME using relative files and HomeToken paths.
// It never runs Driga, invokes Git, reads the real HOME, or writes expected.json.
// Callers should use a fresh destination; unrelated files are left untouched.
func Write(dir string, sc Scenario) error {
	if sc.Now.IsZero() {
		sc.Now = time.Date(2026, 10, 18, 18, 0, 0, 0, time.UTC)
	}
	sc.Now = sc.Now.UTC()
	if sc.Layout == "" {
		sc.Layout = "groves"
	}
	if sc.Repos < 0 || sc.WorktreesPerRepo < 0 || !safeRelative(sc.Layout) {
		return fmt.Errorf("invalid scenario repository layout")
	}
	if sc.Repos > 0 && sc.WorktreesPerRepo == 0 {
		sc.WorktreesPerRepo = 1
	}
	for i, a := range sc.Agents {
		if (a.Adapter != "claude" && a.Adapter != "codex") || a.Repo < 0 || a.Repo >= sc.Repos || a.Worktree < 0 || a.Worktree >= sc.WorktreesPerRepo || (a.ID != "" && (strings.ContainsAny(a.ID, `/\`) || a.ID == "." || a.ID == "..")) || (a.Edit != "" && !safeRelative(a.Edit)) || a.ActivityAge < 0 {
			return fmt.Errorf("invalid agent %d", i)
		}
	}
	w := writer{dir: dir, now: sc.Now, mtimes: map[string]time.Time{}}
	w.json("scenario.json", sc)
	for r := 0; r < sc.Repos; r++ {
		w.repo(sc, r)
	}
	rng := rand.New(rand.NewSource(sc.Seed))
	procs := append([]model.Proc{}, sc.Procs...)
	for i, a := range sc.Agents {
		if a.ID == "" {
			a.ID = fmt.Sprintf("synthetic-%s-%016x", a.Adapter, rng.Uint64())
		}
		if a.Version == "" {
			if a.Adapter == "claude" {
				a.Version = "2.1.90"
			} else {
				a.Version = "0.16.90"
			}
		}
		if a.Status == "" {
			a.Status = "busy"
		}
		age := a.ActivityAge
		if age == 0 {
			age = 20 * time.Second
			if sc.LongIdle {
				age = 2 * time.Hour
			}
		}
		at := sc.Now.Add(-age)
		start := at.Add(-time.Hour)
		pid := int32(4000 + i)
		cwd := filepath.ToSlash(filepath.Join(HomeToken, worktreePath(sc, a.Repo, a.Worktree)))
		proc := model.Proc{PID: pid, PPID: 200, StartTime: start, RSS: uint64(40+rng.Intn(60)) << 20, CPUSeconds: 3, Cmdline: a.Adapter, Exe: "/opt/heldout/bin/" + a.Adapter, Cwd: cwd}
		if a.PIDReuse {
			proc.StartTime = start.Add(time.Minute)
			proc.Cmdline, proc.Exe = "unrelated-worker", "/usr/bin/unrelated-worker"
		}
		if !sc.Crashes && !a.Crashed && !a.Hosted {
			procs = append(procs, proc)
		}
		var log bytes.Buffer
		line := func(v any) {
			b, err := json.Marshal(v)
			if err != nil {
				w.err = err
				return
			}
			log.Write(b)
			log.WriteByte('\n')
		}
		var path string
		if a.Adapter == "claude" {
			w.json(fmt.Sprintf(".claude/sessions/%d.json", pid), map[string]any{"sessionId": a.ID, "cwd": cwd, "name": "Heldout synthetic session", "kind": "interactive", "entrypoint": "cli", "version": a.Version, "startedAt": start, "status": a.Status, "statusUpdatedAt": at})
			path = filepath.Join(".claude/projects/heldout-grove", a.ID+".jsonl")
			line(map[string]any{"type": "user", "cwd": cwd, "gitBranch": "heldout", "timestamp": at.Add(-2 * time.Second), "message": map[string]any{"content": "Synthetic private prompt"}})
			line(map[string]any{"type": "ai-title", "timestamp": at.Add(-time.Second), "aiTitle": "Inspect synthetic grove"})
			if a.Edit != "" {
				line(map[string]any{"type": "assistant", "timestamp": at, "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "name": "Edit", "input": map[string]any{"file_path": a.Edit}}}}})
			}
		} else {
			path = filepath.Join(".codex/sessions", sc.Now.Format("2006/01/02"), "rollout-heldout-"+a.ID+".jsonl")
			source := "cli"
			if a.Hosted {
				source = "desktop"
			}
			line(map[string]any{"type": "session_meta", "timestamp": start, "payload": map[string]any{"id": a.ID, "cwd": cwd, "originator": "heldout_" + source, "source": source, "cli_version": a.Version, "git": map[string]any{"branch": "heldout"}}})
			line(map[string]any{"type": "event_msg", "timestamp": at.Add(-2 * time.Second), "payload": map[string]any{"type": "task_started"}})
			if a.Edit != "" {
				line(map[string]any{"type": "response_item", "timestamp": at, "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Update File: " + a.Edit + "\n*** End Patch"}})
			}
			if a.Completed {
				line(map[string]any{"type": "event_msg", "timestamp": at, "payload": map[string]any{"type": "task_complete", "last_agent_message": "Synthetic private handoff"}})
			}
		}
		if sc.Rotations {
			w.put(path+".1", []byte("{\"type\":\"rotated-predecessor\"}\n"), at.Add(-time.Hour))
		}
		if sc.PartialLines {
			log.WriteString("invalid json\n{\"type\":\"future-unknown-type\"}\n")
			log.WriteString("{\"type\":\"unfinished")
		}
		w.put(path, log.Bytes(), at)
	}
	for i := range procs {
		if procs[i].Cwd == "" && sc.Repos > 0 {
			procs[i].Cwd = filepath.ToSlash(filepath.Join(HomeToken, worktreePath(sc, 0, 0)))
		}
	}
	w.json("procs.json", procs)
	w.json("machine.json", model.MachineSample{TS: sc.Now, Cores: 8, MemTotal: 16 << 30, MemUsed: 6 << 30, SwapTotal: 2 << 30, DiskFree: 80 << 30, PressureFree: 65, Load1: 0.5})
	// Git checkouts do not preserve mtimes. The loader restores these explicitly.
	w.json("mtimes.json", w.mtimes)
	return w.err
}

func safeRelative(p string) bool {
	p = filepath.Clean(p)
	return p != "." && p != ".." && !filepath.IsAbs(p) && !strings.HasPrefix(p, ".."+string(filepath.Separator))
}

func worktreePath(sc Scenario, repo, wt int) string {
	base := filepath.Join(sc.Layout, fmt.Sprintf("grove-%03d", repo))
	if wt == 0 {
		return base
	}
	return base + fmt.Sprintf("-branch-%03d", wt)
}

type writer struct {
	dir    string
	now    time.Time
	err    error
	mtimes map[string]time.Time
}

func (w *writer) put(path string, b []byte, at time.Time) {
	if w.err != nil {
		return
	}
	// A literal .git cannot be checked into a fixture corpus. Store its path
	// components as git.fixture; the test loader restores them in its temp HOME.
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := range parts {
		if parts[i] == ".git" {
			parts[i] = "git.fixture"
		}
	}
	stored := strings.Join(parts, "/")
	p := filepath.Join(w.dir, filepath.FromSlash(stored))
	if w.err = os.MkdirAll(filepath.Dir(p), 0700); w.err != nil {
		return
	}
	if w.err = os.WriteFile(p, b, 0600); w.err != nil {
		return
	}
	w.err = os.Chtimes(p, at, at)
	w.mtimes[stored] = at
}

func (w *writer) json(path string, v any) {
	if w.err != nil {
		return
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		w.err = err
		return
	}
	w.put(path, append(b, '\n'), w.now)
}

// repo writes ordinary Git objects rather than invoking a user's Git hooks or
// configuration. Linked worktrees use relative commondir and portable gitdirs.
func (w *writer) repo(sc Scenario, r int) {
	base := worktreePath(sc, r, 0)
	git := filepath.Join(base, ".git")
	object := func(kind, body string) string {
		raw := []byte(fmt.Sprintf("%s %d\x00%s", kind, len(body), body))
		hash := sha1.Sum(raw)
		id := hex.EncodeToString(hash[:])
		var b bytes.Buffer
		z := zlib.NewWriter(&b)
		if _, err := z.Write(raw); err != nil {
			w.err = err
		}
		if err := z.Close(); err != nil {
			w.err = err
		}
		w.put(filepath.Join(git, "objects", id[:2], id[2:]), b.Bytes(), w.now)
		return id
	}
	tree := object("tree", "")
	commit := object("commit", fmt.Sprintf("tree %s\nauthor Fixture <fixture@example.invalid> %d +0000\ncommitter Fixture <fixture@example.invalid> %d +0000\n\nSynthetic heldout baseline\n", tree, sc.Now.Unix()-86400, sc.Now.Unix()-86400))
	w.put(filepath.Join(git, "config"), []byte("[core]\n repositoryformatversion = 0\n bare = false\n logallrefupdates = false\n"), w.now)
	w.put(filepath.Join(git, "HEAD"), []byte("ref: refs/heads/heldout\n"), w.now)
	w.put(filepath.Join(git, "refs/heads/heldout"), []byte(commit+"\n"), w.now)
	for wt := 1; wt < sc.WorktreesPerRepo; wt++ {
		path := worktreePath(sc, r, wt)
		admin := filepath.Join(git, "worktrees", fmt.Sprintf("branch-%03d", wt))
		w.put(filepath.Join(path, ".git"), []byte("gitdir: "+filepath.ToSlash(filepath.Join(HomeToken, admin))+"\n"), w.now)
		w.put(filepath.Join(admin, "gitdir"), []byte(filepath.ToSlash(filepath.Join(HomeToken, path, ".git"))+"\n"), w.now)
		w.put(filepath.Join(admin, "commondir"), []byte("../..\n"), w.now)
		w.put(filepath.Join(admin, "HEAD"), []byte("ref: refs/heads/heldout\n"), w.now)
	}
}
