package collector

import (
	"path/filepath"
	"testing"

	"github.com/labstack/driga/internal/model"
)

func TestInventoryPathsPreserveEveryRawKey(t *testing.T) {
	root := t.TempDir()
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	alias := root + string(filepath.Separator) + "."
	paths := normalizedInventoryPaths([]model.Session{{SessionObs: model.SessionObs{Cwd: root}}, {SessionObs: model.SessionObs{Cwd: alias}}}, []model.Worktree{{Path: alias}}, nil)
	if len(paths) != 2 || paths[root] != canonical || paths[alias] != canonical {
		t.Fatalf("paths=%v", paths)
	}
}

func BenchmarkNormalizeCollectedPaths(b *testing.B) {
	root := b.TempDir()
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		b.Fatal(err)
	}
	sessions := make([]model.Session, 1000)
	for i := range sessions {
		sessions[i].Cwd = root
	}
	b.ReportAllocs()
	for b.Loop() {
		if got := normalizedInventoryPaths(sessions, nil, nil); got[root] != canonical {
			b.Fatal(got)
		}
	}
}
