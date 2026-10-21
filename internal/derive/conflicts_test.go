package derive

import (
	"github.com/labstack/driga/internal/pathx"
	"path"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func conflictTestSession(id int64, cwd string) model.Session {
	return model.Session{ID: id, Label: "working", SessionObs: model.SessionObs{Cwd: cwd, Branch: "main"}}
}

func conflictTestWorktree(p string) (repo, worktree, rel string) {
	p = path.Clean(p)
	for _, wt := range []string{"/repo/wt1", "/repo/wt2"} {
		if p == wt || strings.HasPrefix(p, wt+"/") {
			rel, _ = pathx.RecordedRel(wt, p)
			return "/repo", wt, rel
		}
	}
	return "", "", ""
}

func conflictTestKind(cs []model.Conflict, kind string) []model.Conflict {
	var result []model.Conflict
	for _, c := range cs {
		if c.Kind == kind {
			result = append(result, c)
		}
	}
	return result
}

func TestSameWorktree(t *testing.T) {
	now := time.Now()
	sessions := []model.Session{conflictTestSession(2, "/repo/wt1/sub"), conflictTestSession(1, "/repo/wt1")}
	got := Conflicts(sessions, nil, nil, conflictTestWorktree, now)
	want := []model.Conflict{{Kind: "same_worktree", Current: true, Path: "/repo/wt1", SessionIDs: []int64{1, 2}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	sessions[0].Cwd, sessions[1].Cwd = "/unknown/sub/..", "/unknown/"
	got = Conflicts(sessions, nil, nil, conflictTestWorktree, now)
	if len(got) != 1 || got[0].Path != "/unknown" {
		t.Fatalf("normalized fallback: %+v", got)
	}
}

func TestSameWorktreeSuppressedInFamily(t *testing.T) {
	sessions := []model.Session{conflictTestSession(3, "/repo/wt1"), conflictTestSession(2, "/repo/wt1")}
	parents := map[int64]int64{3: 2, 2: 1}
	if got := Conflicts(sessions, nil, parents, conflictTestWorktree, time.Now()); len(got) != 0 {
		t.Fatalf("family conflict: %+v", got)
	}
	sessions = append(sessions, conflictTestSession(4, "/repo/wt1"))
	if got := Conflicts(sessions, nil, parents, conflictTestWorktree, time.Now()); len(got) != 1 {
		t.Fatalf("independent session missing: %+v", got)
	}
}

func TestSameFileCurrentVsRecent(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	sessions := []model.Session{conflictTestSession(2, "/repo/wt1"), conflictTestSession(1, "/repo/wt1")}
	for _, tt := range []struct {
		name             string
		age              time.Duration
		current, include bool
	}{
		{"current", 10 * time.Minute, true, true}, {"boundary current", 30 * time.Minute, true, true},
		{"recent", 3 * time.Hour, false, true}, {"boundary recent", 6 * time.Hour, false, true}, {"expired", 6*time.Hour + time.Second, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := map[int64][]model.FileTouch{1: {{Path: "/repo/wt1/a.go", TS: now.Add(-time.Minute)}, {Path: "/repo/wt1/a.go", TS: now.Add(-2 * time.Minute)}}, 2: {{Path: "a.go", TS: now.Add(-tt.age)}}}
			got := conflictTestKind(Conflicts(sessions, files, map[int64]int64{2: 1}, conflictTestWorktree, now), "same_file")
			if !tt.include {
				if len(got) != 0 {
					t.Fatalf("expired: %+v", got)
				}
				return
			}
			if len(got) != 1 || got[0].Current != tt.current || !got[0].Family || !reflect.DeepEqual(got[0].SessionIDs, []int64{1, 2}) {
				t.Fatalf("got %+v", got)
			}
		})
	}
	files := map[int64][]model.FileTouch{1: {{Path: "/repo/wt1/a.go", TS: now}, {Path: "/repo/wt1/a.go", TS: now.Add(-time.Minute)}}}
	if got := conflictTestKind(Conflicts(sessions, files, nil, conflictTestWorktree, now), "same_file"); len(got) != 0 {
		t.Fatalf("one writer: %+v", got)
	}
}

func TestSameFileOtherWorktree(t *testing.T) {
	now := time.Now()
	sessions := []model.Session{conflictTestSession(2, "/repo/wt2"), conflictTestSession(1, "/repo/wt1")}
	files := map[int64][]model.FileTouch{1: {{Path: "/repo/wt1/a.go", TS: now.Add(-10 * time.Minute)}}, 2: {{Path: "/repo/wt2/a.go", TS: now.Add(-20 * time.Minute)}}}
	got := Conflicts(sessions, files, nil, conflictTestWorktree, now)
	if len(got) != 1 || got[0].Kind != "same_file_other_worktree" || !got[0].Current || got[0].Family || got[0].Path != "/repo/a.go" || !reflect.DeepEqual(got[0].SessionIDs, []int64{1, 2}) {
		t.Fatalf("got %+v", got)
	}
	files[2][0].TS = now.Add(-3 * time.Hour)
	got = Conflicts(sessions, files, map[int64]int64{2: 1}, conflictTestWorktree, now)
	if len(got) != 1 || got[0].Current || !got[0].Family {
		t.Fatalf("recent family: %+v", got)
	}
	sessions[0].Branch = "other"
	if got := Conflicts(sessions, files, nil, conflictTestWorktree, now); len(got) != 0 {
		t.Fatalf("different branches: %+v", got)
	}
	sessions[0].Branch = "main"
	files[2][0].TS = now.Add(-7 * time.Hour)
	if got := Conflicts(sessions, files, nil, conflictTestWorktree, now); len(got) != 0 {
		t.Fatalf("expired: %+v", got)
	}
}

func TestDeadSessionsIgnored(t *testing.T) {
	now := time.Now()
	for _, label := range []string{"dead", "finished", "working"} {
		sessions := []model.Session{conflictTestSession(1, "/repo/wt1"), conflictTestSession(2, "/repo/wt1")}
		sessions[1].Label = label
		if label == "working" {
			sessions[1].EndedAt = &now
		}
		files := map[int64][]model.FileTouch{1: {{Path: "/repo/wt1/a", TS: now}}, 2: {{Path: "/repo/wt1/a", TS: now}}}
		if got := Conflicts(sessions, files, nil, conflictTestWorktree, now); len(got) != 0 {
			t.Fatalf("%s: %+v", label, got)
		}
	}
}

func TestConflictDeterministic(t *testing.T) {
	now := time.Now()
	sessions := []model.Session{conflictTestSession(3, "/repo/wt2"), conflictTestSession(2, "/repo/wt1"), conflictTestSession(1, "/repo/wt1")}
	files := map[int64][]model.FileTouch{}
	for _, s := range sessions {
		for _, path := range []string{"b", "a"} {
			files[s.ID] = append(files[s.ID], model.FileTouch{Path: pathx.RecordedJoin(s.Cwd, path), TS: now})
		}
	}
	want := Conflicts(sessions, files, nil, conflictTestWorktree, now)
	for i := 0; i < 20; i++ {
		sessions[0], sessions[2] = sessions[2], sessions[0]
		if got := Conflicts(sessions, files, nil, conflictTestWorktree, now); !reflect.DeepEqual(got, want) {
			t.Fatalf("unstable: %+v vs %+v", got, want)
		}
	}
	for i := 1; i < len(want); i++ {
		a, b := want[i-1], want[i]
		if a.Kind > b.Kind || a.Kind == b.Kind && a.Path > b.Path {
			t.Fatalf("unsorted: %+v", want)
		}
	}
}
