package derive

import (
	"github.com/labstack/driga/internal/pathx"
	"strings"
	"time"

	"github.com/labstack/driga/internal/model"
)

// ProjectStatus is a repository's activity, risks, and sourced summary.
type ProjectStatus struct {
	Repo       model.Repo
	Activity   string
	AtRisk     []string
	LiveAgents int
	Where      []WherePart
}

// WherePart is one timestamped handoff, session title, or commit subject.
type WherePart struct {
	Source string
	Text   string
	At     time.Time
}

// Project summarizes observations belonging to repo. Handoffs are repo-scoped
// by the caller because completion events do not carry a repository ID.
func Project(repo model.Repo, wts []model.Worktree, sessions []model.Session, handoffs []model.CompletionEvent, now time.Time) ProjectStatus {
	status := ProjectStatus{Repo: repo, Activity: "idle"}
	var lastActivity time.Time
	var handoff, title, commit WherePart
	var dirty, unpushed bool
	worktreeCount := 0
	for _, wt := range wts {
		if wt.RepoID != repo.ID {
			continue
		}
		worktreeCount++
		if wt.LastCommitAt.After(lastActivity) {
			lastActivity = wt.LastCommitAt
		}
		if (wt.Dirty > 0 || wt.Untracked > 0) && !wt.OldestDirtyMtime.IsZero() && now.Sub(wt.OldestDirtyMtime) > 7*24*time.Hour {
			dirty = true
		}
		if wt.Ahead > 0 && !wt.LastCommitAt.IsZero() && now.Sub(wt.LastCommitAt) > 3*24*time.Hour {
			unpushed = true
		}
		projectWhereLatest(&commit, "commit", wt.LastCommitSubject, wt.LastCommitAt)
	}
	for _, s := range sessions {
		if !projectSessionInRepo(s, repo, wts) {
			continue
		}
		if s.EndedAt == nil && s.Label != "dead" && s.Label != "ended" && s.Label != "finished" {
			status.LiveAgents++
		}
		if s.LastActivityAt.After(lastActivity) {
			lastActivity = s.LastActivityAt
		}
		projectWhereLatest(&title, "title", s.Title, s.LastActivityAt)
	}
	if status.LiveAgents > 0 || !lastActivity.IsZero() && now.Sub(lastActivity) < 24*time.Hour {
		status.Activity = "active"
	} else if !lastActivity.IsZero() && now.Sub(lastActivity) < 7*24*time.Hour {
		status.Activity = "recent"
	}
	if dirty {
		status.AtRisk = append(status.AtRisk, "uncommitted >7d")
	}
	if unpushed {
		status.AtRisk = append(status.AtRisk, "unpushed >3d")
	}
	if worktreeCount > 10 {
		status.AtRisk = append(status.AtRisk, ">10 worktrees")
	}
	for _, h := range handoffs {
		projectWhereLatest(&handoff, "handoff", h.Text, h.TS)
	}
	for _, part := range []WherePart{handoff, title, commit} {
		if part.Text != "" {
			status.Where = append(status.Where, part)
		}
	}
	return status
}

func projectWhereLatest(part *WherePart, source, text string, at time.Time) {
	if strings.TrimSpace(text) == "" {
		return
	}
	if part.Text == "" || at.After(part.At) || at.Equal(part.At) && text < part.Text {
		*part = WherePart{Source: source, Text: text, At: at}
	}
}

func projectSessionInRepo(s model.Session, repo model.Repo, wts []model.Worktree) bool {
	if s.RepoID != nil {
		return *s.RepoID == repo.ID
	}
	for _, wt := range wts {
		if wt.RepoID == repo.ID && (s.WorktreeID != nil && *s.WorktreeID == wt.ID || projectPathWithin(s.Cwd, wt.Path)) {
			return true
		}
	}
	return projectPathWithin(s.Cwd, repo.Path)
}

func projectPathWithin(path, root string) bool { return pathx.RecordedWithin(path, root) }
