package derive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/scrub"
)

// SummaryInput is the complete allowlist of data sent to Claude. Paths,
// transcript messages, prompt snippets and file contents have no field here.
type SummaryInput struct {
	Project   string            `json:"project"`
	Worktrees []SummaryWorktree `json:"worktrees"`
	Titles    []string          `json:"titles"`
	Handoffs  []string          `json:"handoffs,omitempty"`
}
type SummaryWorktree struct {
	Branch  string   `json:"branch"`
	Dirty   int      `json:"dirty"`
	Ahead   int      `json:"ahead"`
	Behind  int      `json:"behind"`
	Commits []string `json:"commits"`
}

// BuildSummaryInput uses commit subjects provided by the caller. Without a
// live git log it uses the collector's latest subject for freshness comparison.
func BuildSummaryInput(repo model.Repo, wts []model.Worktree, ss []model.Session, hs []model.CompletionEvent, now time.Time, snippets bool, commits map[int64][]string) SummaryInput {
	in := SummaryInput{Project: scrub.Scrub(repo.Name), Worktrees: []SummaryWorktree{}, Titles: []string{}}
	for _, w := range wts {
		if w.RepoID != repo.ID {
			continue
		}
		subjects := commits[w.ID]
		if subjects == nil {
			subjects = []string{}
			if w.LastCommitSubject != "" {
				subjects = append(subjects, w.LastCommitSubject)
			}
		}
		clean := make([]string, len(subjects))
		for i, s := range subjects {
			clean[i] = scrub.Scrub(s)
		}
		in.Worktrees = append(in.Worktrees, SummaryWorktree{scrub.Scrub(w.Branch), w.Dirty + w.Untracked, w.Ahead, w.Behind, clean})
	}
	sort.Slice(in.Worktrees, func(i, j int) bool {
		a, _ := json.Marshal(in.Worktrees[i])
		b, _ := json.Marshal(in.Worktrees[j])
		return string(a) < string(b)
	})
	for _, s := range ss {
		if projectSessionInRepo(s, repo, wts) && !s.LastActivityAt.Before(now.Add(-24*time.Hour)) && s.Title != "" {
			in.Titles = append(in.Titles, scrub.Scrub(s.Title))
		}
	}
	sort.Strings(in.Titles)
	if snippets {
		for _, h := range hs {
			if h.Text != "" {
				in.Handoffs = append(in.Handoffs, scrub.Snippet(h.Text))
			}
		}
		sort.Strings(in.Handoffs)
	}
	return in
}

func SummaryInputHash(input SummaryInput) string {
	b, _ := json.Marshal(input)
	return summaryDigest(b)
}
func summaryDigest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

// SummaryStateHash also tracks HEAD, so a new commit with an unchanged subject
// invalidates the summary. It ignores sample timestamps and local row IDs.
func SummaryStateHash(repo model.Repo, wts []model.Worktree, ss []model.Session, hs []model.CompletionEvent, now time.Time, snippets bool) string {
	heads := []string{}
	for _, w := range wts {
		if w.RepoID == repo.ID {
			heads = append(heads, w.Head)
		}
	}
	sort.Strings(heads)
	b, _ := json.Marshal(struct {
		Input SummaryInput
		Heads []string
	}{BuildSummaryInput(repo, wts, ss, hs, now, snippets, nil), heads})
	return summaryDigest(b)
}
