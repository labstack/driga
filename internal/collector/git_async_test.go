package collector

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/driga/internal/gitscan"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

type stalledGit struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *stalledGit) Scan(ctx context.Context, path string, _ gitscan.ScanMode) ([]model.Worktree, error) {
	s.once.Do(func() { close(s.started) })
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.release:
		return []model.Worktree{{Path: path, Branch: "main"}}, nil
	}
}
func (*stalledGit) Prune([]string) {}

func TestSlowGitDoesNotBlockSamplingOrIntake(t *testing.T) {
	d, _, plat, _ := setup(t)
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	d.Cfg.Roots = []string{repo}
	scan := &stalledGit{started: make(chan struct{}), release: make(chan struct{})}
	d.Git = scan
	clock := &daemonTestClock{now: d.now(), requests: make(chan *daemonTestTimer)}
	d.clock, d.Now = clock, clock.Now
	d.Cfg.Intake.OTLP, d.Cfg.Intake.Listen = true, "127.0.0.1:0"
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, d, dir) }()
	t.Cleanup(func() {
		cancel()
		close(scan.release)
		for {
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
				return
			case timer := <-clock.requests:
				timer.ticks <- clock.Now()
			case <-time.After(5 * time.Second):
				t.Error("collector did not stop")
				return
			}
		}
	})
	select {
	case <-scan.started:
	case <-time.After(3 * time.Second):
		t.Fatal("Git scan never started")
	}
	var timer *daemonTestTimer
	select {
	case timer = <-clock.requests:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("stalled Git blocks process publication and intake loop")
	}
	st, err := store.Open(filepath.Join(dir, "driga.db"), store.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if procs, err := st.Procs(); err != nil || len(procs) == 0 {
		t.Fatalf("processes not published: %v %v", procs, err)
	}
	addr, err := st.Meta("otlp:listen")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile("../intake/testdata/claude/events.json")
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Post("http://"+addr+"/v1/logs", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("Git blocked telemetry: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("intake status %d", resp.StatusCode)
	}
	// Intake wakes the daemon without advancing time. Drain its next timer,
	// then trigger another process cadence while the same Git job is pending.
	select {
	case timer = <-clock.requests:
	case <-time.After(time.Second):
		t.Fatal("intake loop stalled")
	}
	clock.advance(time.Minute)
	timer.ticks <- clock.Now()
	select {
	case <-clock.requests:
	case <-time.After(time.Second):
		t.Fatal("slow Git blocked next process cadence")
	}
	if plat.procCalls < 2 {
		t.Fatal("process sampling did not continue")
	}
}

func TestGitResultCommitsWithoutRepollingAdapters(t *testing.T) {
	d, st, _, a := setup(t)
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	d.Cfg.Roots = []string{repo}
	scan := &stalledGit{started: make(chan struct{}), release: make(chan struct{})}
	d.Git = scan
	d.gitWork = newGitWorker(context.Background(), d.now)
	defer d.gitWork.close()
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		id, err := tx.UpsertRepo(model.Repo{Path: repo, Name: "fixture"})
		if err != nil {
			return err
		}
		_, err = tx.UpsertWorktree(model.Worktree{RepoID: id, Path: repo, Branch: "old"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{Git: true, GitFull: true})
	ws, _ := st.Worktrees(0)
	if len(ws) != 1 || ws[0].Branch != "old" {
		t.Fatalf("pending inventory lost: %+v", ws)
	}
	close(scan.release)
	select {
	case <-d.gitWork.wake:
	case <-time.After(time.Second):
		t.Fatal("Git result did not finish")
	}
	if full, err := applyReadyGit(context.Background(), d, st); err != nil || !full {
		t.Fatalf("commit: full=%t err=%v", full, err)
	}
	ws, _ = st.Worktrees(0)
	if len(ws) != 1 || ws[0].Branch != "main" {
		t.Fatalf("completed inventory not applied: %+v", ws)
	}
	if len(a.envs) != 1 || d.gitWork.isReady() {
		t.Fatalf("completion repolled adapters or retained result: polls=%d ready=%t", len(a.envs), d.gitWork.isReady())
	}
}
