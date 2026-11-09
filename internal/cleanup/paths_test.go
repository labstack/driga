//go:build !windows

package cleanup

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

// A listing inspects many candidates against the same process observation.
// Adding unrelated processes must not repeat filesystem path work per candidate.
func TestBuildInspectionReusesObservedPaths(t *testing.T) {
	e, p, repo, _ := buildFixture(t)
	ctx := context.Background()
	state, err := readCheckoutState(ctx, e, repo)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	measure := func(n int) float64 {
		p.procs = nil
		p.openPaths = nil
		for i := 0; i < n; i++ {
			p.procs = append(p.procs, model.Proc{PID: int32(i + 1), Cwd: cwd, Cmdline: "shell"})
			p.openPaths = append(p.openPaths, filepath.Join(cwd, "file"))
		}
		s, err := read(ctx, e)
		if err != nil {
			t.Fatal(err)
		}
		return testing.AllocsPerRun(2, func() {
			for i := 0; i < 3; i++ {
				if _, err := inspectBuild(ctx, e, s, state, repo, "node_modules/"); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	small, large := measure(1), measure(400)
	t.Logf("candidate allocations: one process=%.0f, 400 processes=%.0f", small, large)
	if large > small+100 {
		t.Fatalf("repeated candidate inspections allocate per unrelated process: one=%.0f, 400=%.0f", small, large)
	}
}

func TestObservedPathsKeepAliasesAndBoundaries(t *testing.T) {
	root := resolved(t.TempDir())
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(alias, "missing", "child")
	s := snapshot{
		cwds: map[int32]string{1: child, 2: "", 3: "relative"},
		open: []string{child},
		sessions: []model.Session{
			{SessionObs: model.SessionObs{Cwd: alias, ExternalID: "root"}},
			{SessionObs: model.SessionObs{Cwd: child, ExternalID: "child"}},
			{SessionObs: model.SessionObs{Cwd: root + "-other", ExternalID: "sibling"}},
			{SessionObs: model.SessionObs{Cwd: "relative", ExternalID: "relative"}},
		},
	}
	s.preparePaths()
	for _, path := range []string{root, filepath.Join(root, "missing")} {
		if !s.cwdUnder(path) {
			t.Fatalf("cwd alias lost under %s", path)
		}
		if open, err := openUnder(s, path); err != nil || !open {
			t.Fatalf("open alias lost: %v %v", open, err)
		}
	}
	if s.cwdUnder(root+"-other") || s.cwdUnder("relative") {
		t.Fatal("containment crossed a boundary")
	}
	ids := []string{}
	for session := range s.sessionsUnder(root) {
		ids = append(ids, session.ExternalID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"child", "root"}) {
		t.Fatalf("sessions: %v", ids)
	}
	if s.sessions[0].Cwd != alias {
		t.Fatal("scratch attribution spelling changed")
	}
	if open, err := openUnder(s, alias); err != nil || !open {
		t.Fatalf("native open spelling lost: %v %v", open, err)
	}
}

func TestBuildVerifyRefreshesObservedAliases(t *testing.T) {
	for _, observation := range []string{"cwd", "open", "session"} {
		t.Run(observation, func(t *testing.T) {
			e, p, _, path := buildFixture(t)
			alias := filepath.Join(t.TempDir(), "alias")
			if err := os.Symlink(t.TempDir(), alias); err != nil {
				t.Fatal(err)
			}
			if observation == "cwd" {
				p.procs = []model.Proc{{PID: 42, Cwd: filepath.Join(alias, "missing"), Cmdline: "shell"}}
			} else if observation == "open" {
				p.openPaths = []string{filepath.Join(alias, "missing")}
			} else {
				tx(t, e, func(tx *store.Tx) error {
					_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "alias-test", Cwd: filepath.Join(alias, "missing")}, Label: "working"})
					return err
				})
			}
			c := listed(t, e, path)
			if err := os.Remove(alias); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(path, alias); err != nil {
				t.Fatal(err)
			}
			err := Verify(context.Background(), e, c)
			if err == nil || !strings.Contains(err.Error(), "inside") && !strings.Contains(err.Error(), "live session") {
				t.Fatalf("new alias was not protected: %v", err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal(err)
			}
		})
	}
}
