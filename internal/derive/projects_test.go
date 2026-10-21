package derive

import (
	"reflect"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestActivityBuckets(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	repo := model.Repo{ID: 1, Path: "/repo"}
	repoID := repo.ID
	otherID := int64(2)
	for _, tt := range []struct {
		name  string
		age   time.Duration
		label string
		ended bool
		want  string
		live  int
	}{
		{"live", 30 * 24 * time.Hour, "working", false, "active", 1},
		{"unknown live", 30 * 24 * time.Hour, "unknown", false, "active", 1},
		{"activity", time.Hour, "dead", false, "active", 0},
		{"recent", 3 * 24 * time.Hour, "finished", false, "recent", 0},
		{"active boundary", 24 * time.Hour, "dead", false, "recent", 0},
		{"idle", 30 * 24 * time.Hour, "dead", false, "idle", 0},
		{"recent boundary", 7 * 24 * time.Hour, "dead", false, "idle", 0},
		{"ended", 30 * 24 * time.Hour, "working", true, "idle", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := model.Session{RepoID: &repoID, Label: tt.label, SessionObs: model.SessionObs{LastActivityAt: now.Add(-tt.age)}}
			if tt.ended {
				s.EndedAt = &now
			}
			other := model.Session{RepoID: &otherID, Label: "working", SessionObs: model.SessionObs{LastActivityAt: now}}
			got := Project(repo, nil, []model.Session{s, other}, nil, now)
			if got.Activity != tt.want || got.LiveAgents != tt.live || got.Repo != repo {
				t.Fatalf("got %+v", got)
			}
		})
	}
	got := Project(repo, []model.Worktree{{RepoID: 1, LastCommitAt: now.Add(-time.Hour)}}, nil, nil, now)
	if got.Activity != "active" {
		t.Fatalf("commit activity: %+v", got)
	}
	got = Project(repo, []model.Worktree{{RepoID: 2, LastCommitAt: now}}, nil, nil, now)
	if got.Activity != "idle" {
		t.Fatalf("foreign worktree: %+v", got)
	}
}

func TestAtRisk(t *testing.T) {
	now := time.Now()
	repo := model.Repo{ID: 1}
	for _, tt := range []struct {
		name string
		wt   model.Worktree
		want []string
	}{
		{"dirty", model.Worktree{Dirty: 1, OldestDirtyMtime: now.Add(-8 * 24 * time.Hour)}, []string{"uncommitted >7d"}},
		{"untracked", model.Worktree{Untracked: 1, OldestDirtyMtime: now.Add(-8 * 24 * time.Hour)}, []string{"uncommitted >7d"}},
		{"unpushed", model.Worktree{Ahead: 1, LastCommitAt: now.Add(-4 * 24 * time.Hour)}, []string{"unpushed >3d"}},
		{"clean", model.Worktree{OldestDirtyMtime: now.Add(-8 * 24 * time.Hour), LastCommitAt: now.Add(-4 * 24 * time.Hour)}, nil},
		{"unknown ages", model.Worktree{Dirty: 1, Ahead: 1}, nil},
		{"boundaries", model.Worktree{Dirty: 1, Ahead: 1, OldestDirtyMtime: now.Add(-7 * 24 * time.Hour), LastCommitAt: now.Add(-3 * 24 * time.Hour)}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tt.wt.RepoID = 1
			got := Project(repo, []model.Worktree{tt.wt}, nil, nil, now)
			if !reflect.DeepEqual(got.AtRisk, tt.want) {
				t.Fatalf("got %v want %v", got.AtRisk, tt.want)
			}
		})
	}
	wts := make([]model.Worktree, 11)
	for i := range wts {
		wts[i] = model.Worktree{RepoID: 1, Dirty: 1, Ahead: 1, OldestDirtyMtime: now.Add(-8 * 24 * time.Hour), LastCommitAt: now.Add(-4 * 24 * time.Hour)}
	}
	got := Project(repo, wts, nil, nil, now)
	want := []string{"uncommitted >7d", "unpushed >3d", ">10 worktrees"}
	if !reflect.DeepEqual(got.AtRisk, want) {
		t.Fatalf("got %v want %v", got.AtRisk, want)
	}
	if got := Project(repo, wts[:10], nil, nil, now); len(got.AtRisk) != 2 {
		t.Fatalf("10 worktrees: %v", got.AtRisk)
	}
}

func TestWhereOrderAndSources(t *testing.T) {
	now := time.Now()
	repo := model.Repo{ID: 1}
	id := repo.ID
	handoffs := []model.CompletionEvent{{Text: "old handoff", TS: now.Add(-time.Hour)}, {Text: "latest handoff", TS: now.Add(-time.Minute)}, {Text: "", TS: now}}
	sessions := []model.Session{{RepoID: &id, SessionObs: model.SessionObs{Title: "old title", LastActivityAt: now.Add(-time.Hour)}}, {RepoID: &id, SessionObs: model.SessionObs{Title: "latest title", LastActivityAt: now.Add(-2 * time.Minute)}}}
	wts := []model.Worktree{{RepoID: 1, LastCommitSubject: "old commit", LastCommitAt: now.Add(-24 * time.Hour)}, {RepoID: 1, LastCommitSubject: "latest commit", LastCommitAt: now.Add(-time.Hour)}}
	want := []WherePart{{Source: "handoff", Text: "latest handoff", At: handoffs[1].TS}, {Source: "title", Text: "latest title", At: sessions[1].LastActivityAt}, {Source: "commit", Text: "latest commit", At: wts[1].LastCommitAt}}
	if got := Project(repo, wts, sessions, handoffs, now).Where; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	if got := Project(repo, wts, sessions, nil, now).Where; !reflect.DeepEqual(got, want[1:]) {
		t.Fatalf("without handoff: %+v", got)
	}
	sessions[1].Title = "  "
	if got := Project(repo, wts, sessions, nil, now).Where; len(got) != 2 || got[0].Text != "old title" {
		t.Fatalf("empty title: %+v", got)
	}
	if got := Project(repo, nil, nil, []model.CompletionEvent{{Text: " "}}, now).Where; len(got) != 0 {
		t.Fatalf("empty sources: %+v", got)
	}
}
