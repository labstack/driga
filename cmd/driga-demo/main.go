// Command driga-demo plays a scripted set of agent sessions into a private
// data directory, so the real `driga top` can be filmed for
// the website without showing anyone's machine. It never reads the process
// table, agent files or Git: every row is written here.
//
//	driga-demo --data-dir /tmp/driga-demo
//	DRIGA_DATA_DIR=/tmp/driga-demo DRIGA_CONFIG=/tmp/driga-demo/config.toml driga top
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

const marker = ".driga-demo"

func main() {
	dir := flag.String("data-dir", "", "empty or demo data directory (required)")
	flag.Parse()
	if err := run(*dir); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "driga-demo:", err)
		os.Exit(1)
	}
}

// prepare takes the collector lock on a demo directory and resets it. It
// refuses anything outside the temporary directory and any directory holding
// something other than a demo, so a mistyped path can never touch real Driga
// data, and a second run cannot reset a demo that is still playing.
func prepare(dir string) (*os.File, error) {
	if dir == "" {
		return nil, errors.New("--data-dir is required")
	}
	if !temporary(dir) {
		return nil, fmt.Errorf("%s is outside the temporary directory; demos only run in scratch space", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(dir, marker)); len(entries) > 0 && err != nil {
		return nil, fmt.Errorf("%s is not empty and is not a driga-demo directory", dir)
	}
	lock, err := collector.Lock(dir, collector.Owner{Kind: "manual"})
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Name() == "driga.lock" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			lock.Close()
			return nil, err
		}
	}
	// Prompt input is shown to the viewer only under the snippets policy.
	for name, body := range map[string]string{marker: "", "config.toml": "capture_text = \"snippets\"\nno_service_hint = true\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			lock.Close()
			return nil, err
		}
	}
	return lock, nil
}

func temporary(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for _, root := range []string{os.TempDir(), "/tmp"} {
		root, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
		if err != nil {
			parent = filepath.Dir(abs)
		}
		if rel, err := filepath.Rel(root, filepath.Join(parent, filepath.Base(abs))); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return true
		}
	}
	return false
}

type session struct {
	key, adapter, title, project string
	parent                       string
	label                        string
	start                        time.Duration // before the demo starts
	appear                       time.Duration // after the demo starts
	files                        []string
	pid                          int32
	mem                          uint64
}

var projects = []string{"atlas", "canon", "relay"}

var script = []session{
	{key: "billing", adapter: "claude", title: "Migrate billing to Stripe v3", project: "atlas", label: "working", start: 12 * time.Minute, files: []string{"src/billing.ts", "src/webhooks.ts"}, pid: 4101, mem: 1800 << 20},
	{key: "webhooks", adapter: "claude", title: "Update webhook handlers", project: "atlas", parent: "billing", label: "working", appear: 3 * time.Second, files: []string{"src/webhooks.ts"}},
	{key: "tests", adapter: "claude", title: "Write migration tests", project: "atlas", parent: "billing", label: "working", appear: 5 * time.Second},
	{key: "react", adapter: "codex", title: "Upgrade to React 20", project: "atlas", label: "stuck", start: 41 * time.Minute, pid: 4104, mem: 1200 << 20},
	{key: "upload", adapter: "codex", title: "Fix flaky upload test", project: "canon", label: "working", start: 19 * time.Minute, files: []string{"src/upload.ts"}, pid: 4201, mem: 1100 << 20},
	{key: "darkmode", adapter: "gemini", title: "Add dark mode to settings", project: "canon", label: "working", start: 27 * time.Minute, files: []string{"src/upload.ts", "src/settings.tsx"}, pid: 4202, mem: 800 << 20},
	{key: "notes", adapter: "copilot", title: "Release notes for 2.4", project: "relay", label: "finished", start: 62 * time.Minute, pid: 4301, mem: 300 << 20},
	{key: "triage", adapter: "claude", title: "Triage open issues", project: "relay", label: "idle", start: 34 * time.Minute, pid: 4302, mem: 500 << 20},
}

// The permission prompt arrives after the opening beats of the film.
const promptAt = 8 * time.Second

func run(dir string) error {
	lock, err := prepare(dir)
	if err != nil {
		return err
	}
	defer lock.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	st, err := store.Open(filepath.Join(dir, "driga.db"), store.ReadWrite)
	if err != nil {
		return err
	}
	defer st.Close()
	broker, err := chat.New(ctx, st, noAgent{}, chat.Options{Snippets: true, DataDir: dir})
	if err != nil {
		return err
	}
	defer broker.Close()
	stopChat, err := chat.Listen(ctx, dir, broker)
	if err != nil {
		return err
	}
	defer stopChat()

	t0 := time.Now().UTC()
	d := &demo{st: st, t0: t0, ids: map[string]int64{}, repos: map[string]int64{}}
	if err := st.Tx(ctx, d.seed); err != nil {
		return err
	}
	answered := make(chan bool, 1)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	asked := false
	for {
		now := time.Now().UTC()
		if !asked && now.Sub(t0) >= promptAt {
			asked = true
			if err := d.ask(ctx, broker, answered); err != nil {
				return err
			}
		}
		select {
		case allow := <-answered:
			d.answered(allow) // either answer resumes the session in the script
		default:
		}
		if err := st.Tx(ctx, func(tx *store.Tx) error { return d.pass(tx, now) }); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

type noAgent struct{}

func (noAgent) Run(context.Context, chat.Request, func(chat.Delta) error) error {
	return errors.New("Chat is not part of the demo")
}

type demo struct {
	st       *store.Store
	t0       time.Time
	ids      map[string]int64
	repos    map[string]int64
	waiting  bool // billing is waiting on the prompt
	resolved bool
}

func cwd(project string) string { return "/Users/demo/code/" + project }

func (d *demo) seed(tx *store.Tx) error {
	if err := tx.UpsertMachine(model.Machine{Hostname: "studio", OS: "darwin", Arch: "arm64", Cores: 12, MemTotal: 36 << 30}); err != nil {
		return err
	}
	for _, p := range projects {
		id, err := tx.UpsertRepo(model.Repo{Path: cwd(p), Name: p})
		if err != nil {
			return err
		}
		d.repos[p] = id
		if _, err := tx.UpsertWorktree(model.Worktree{RepoID: id, Path: cwd(p), Branch: "main", Dirty: 3, LastCommitAt: d.t0.Add(-2 * time.Hour), LastCommitSubject: "Prepare " + p + " release", UpdatedAt: d.t0}); err != nil {
			return err
		}
	}
	// An hour of activity history gives every bar its past.
	for _, s := range script {
		if s.appear > 0 {
			continue
		}
		id, err := d.upsert(tx, s, d.t0)
		if err != nil {
			return err
		}
		for m := s.start; m > 0; m -= time.Minute {
			at := d.t0.Add(-m)
			active := s.label == "working" || s.label == "waiting" || (s.label == "stuck" && m > 20*time.Minute) || (s.label == "finished" && m > 45*time.Minute)
			if active && int(m/time.Minute)%7 != 3 {
				if err := tx.Observe(id, at, int64(2000+int(m/time.Minute)%5*900), 400); err != nil {
					return err
				}
			}
		}
	}
	return tx.ReplaceOrphans([]model.Orphan{{ID: "5120:1", RootPID: 5120, StartTime: d.t0.Add(-3 * time.Hour), Signature: "vite dev server", ProcCount: 4, RSSTotal: 900 << 20, Cwd: cwd("canon"), Verified: true, Evidence: []string{"parent session ended 2h ago", "no open connections"}, FirstSeen: d.t0.Add(-2 * time.Hour), PIDs: []int32{5120, 5121, 5122, 5123}}})
}

func (d *demo) upsert(tx *store.Tx, s session, now time.Time) (int64, error) {
	label := s.label
	if s.key == "billing" && d.waiting {
		label = "waiting"
	}
	repo := d.repos[s.project]
	obs := model.SessionObs{Adapter: s.adapter, ExternalID: "demo-" + s.key, Origin: "interactive", PID: s.pid, ProcStart: d.t0.Add(-s.start), Cwd: cwd(s.project), Branch: "main", Title: s.title,
		StartedAt: d.t0.Add(-s.start + s.appear), LastActivityAt: now, TurnOpen: label == "working" || label == "waiting"}
	if label == "stuck" {
		obs.LastActivityAt = d.t0.Add(-20 * time.Minute)
	}
	if label == "finished" || label == "idle" {
		obs.LastActivityAt = d.t0.Add(-s.start + 18*time.Minute)
	}
	session := model.Session{SessionObs: obs, Label: label, Evidence: "demo script", RepoID: &repo}
	if s.parent != "" {
		parent := d.ids[s.parent]
		session.ParentID = &parent
		session.Hosted = true
		session.Origin = ""
	}
	id, err := tx.UpsertSession(session)
	if err != nil {
		return 0, err
	}
	d.ids[s.key] = id
	if len(s.files) > 0 {
		var touches []model.FileTouch
		for _, f := range s.files {
			touches = append(touches, model.FileTouch{Path: filepath.Join(cwd(s.project), f), TS: now})
		}
		if err := tx.ReplaceSessionFiles(id, touches); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// pass rewrites what a collector pass would: sessions, the current minute of
// activity, processes, a machine sample and freshness.
func (d *demo) pass(tx *store.Tx, now time.Time) error {
	var procs []model.Proc
	for _, s := range script {
		if now.Sub(d.t0) < s.appear {
			continue
		}
		id, err := d.upsert(tx, s, now)
		if err != nil {
			return err
		}
		if s.label == "working" && !(s.key == "billing" && d.waiting) {
			// Observe adds into the current minute, so a pass a second adds 1/60th.
			if err := tx.Observe(id, now, 60, 6); err != nil {
				return err
			}
		}
		if s.pid == 0 {
			continue // workers run inside their lead's process
		}
		procs = append(procs, model.Proc{PID: s.pid, PPID: 1, StartTime: d.t0.Add(-s.start), Footprint: s.mem, RSS: s.mem, CPUPercent: map[bool]float64{true: 18, false: 0.4}[s.label == "working"],
			Cmdline: s.adapter, Exe: "/usr/local/bin/" + s.adapter, Cwd: cwd(s.project), Agent: s.adapter, Role: "agent", SessionID: id})
	}
	// A machine always runs other apps; without them the Machines tab is not representative.
	procs = append(procs,
		model.Proc{PID: 900, PPID: 1, StartTime: d.t0.Add(-5 * time.Hour), Footprint: 2600 << 20, RSS: 2600 << 20, CPUPercent: 6, Cmdline: "Safari", Exe: "/Applications/Safari.app/Contents/MacOS/Safari"},
		model.Proc{PID: 901, PPID: 1, StartTime: d.t0.Add(-5 * time.Hour), Footprint: 1900 << 20, RSS: 1900 << 20, CPUPercent: 3, Cmdline: "Code Helper", Exe: "/Applications/Visual Studio Code.app/Contents/MacOS/Electron"})
	if err := tx.ReplaceProcs(procs); err != nil {
		return err
	}
	if err := tx.InsertSample(model.MachineSample{TS: now, MemUsed: 22 << 30, MemCompressed: 3 << 30, MemTotal: 36 << 30, SwapUsed: 1 << 30, SwapTotal: 4 << 30,
		DiskFree: 210_000_000_000, DiskTotal: 994_000_000_000, PressureLevel: "normal", PressureFree: 38, Load1: 6.2, Load5: 5.8, Load15: 5.1, Cores: 12}); err != nil {
		return err
	}
	for _, source := range []string{"procs", "machine", "git", "claude", "codex", "copilot", "gemini"} {
		if err := tx.SetSourceOK(source, now); err != nil {
			return err
		}
	}
	return tx.SetMeta("last_pass_at", now.Format(time.RFC3339Nano))
}

// ask raises the wait the way the PermissionRequest hook does: an inbox item
// plus a pending prompt in the real broker, answered from top or the app.
func (d *demo) ask(ctx context.Context, broker *chat.Broker, answered chan<- bool) error {
	now := time.Now().UTC()
	command := "pnpm prisma migrate deploy --schema packages/db/prisma/schema.prisma"
	input, _ := json.Marshal(map[string]string{"command": command})
	d.waiting = true
	if err := d.st.Tx(ctx, func(tx *store.Tx) error {
		return tx.UpsertInboxItem(model.InboxItem{ID: "demo-billing-permission", SessionID: d.ids["billing"], Kind: "waiting", Reason: "permission", Tool: "Bash",
			Detail: command, TS: now, WaitingSinceAt: now, Summary: "Permission · Bash"})
	}); err != nil {
		return err
	}
	prompt := chat.Prompt{ID: "demo-billing", Session: "demo-billing", Cwd: cwd("atlas"), Tool: "Bash", Input: input}
	go func() {
		decision, err := broker.WaitPrompt(ctx, prompt)
		if err == nil {
			answered <- decision.Behavior == "allow"
		}
	}()
	return nil
}

func (d *demo) answered(_ bool) {
	if d.resolved {
		return
	}
	d.resolved, d.waiting = true, false
	// Resumed activity closes the wait, as a collector pass does.
	_ = d.st.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.CloseAttentionActivity(d.ids["billing"], time.Now().UTC())
	})
}
