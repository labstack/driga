package derive

import (
	"github.com/labstack/driga/internal/pathx"
	"sort"
	"time"

	"github.com/labstack/driga/internal/model"
)

type conflictEdit struct {
	id, root int64
	worktree string
	at       time.Time
}

type conflictFileKey struct {
	repo, rel, branch string
}

// Conflicts finds live sessions sharing a worktree or recently edited files.
func Conflicts(sessions []model.Session, files map[int64][]model.FileTouch, parents map[int64]int64, worktreeOf func(path string) (repo, worktree, rel string), now time.Time) []model.Conflict {
	worktrees := make(map[string]map[int64]conflictEdit)
	absoluteFiles := make(map[string]map[int64]conflictEdit)
	otherFiles := make(map[conflictFileKey]map[int64]conflictEdit)
	for _, s := range sessions {
		if s.EndedAt != nil || s.Label == "dead" || s.Label == "finished" {
			continue
		}
		root := conflictRoot(s.ID, parents)
		var repo, wt string
		if worktreeOf != nil && s.Cwd != "" {
			repo, wt, _ = worktreeOf(s.Cwd)
		}
		if wt == "" {
			wt = s.Cwd
		}
		if wt != "" {
			wt = pathx.RecordedKey(wt)
			if worktrees[wt] == nil {
				worktrees[wt] = make(map[int64]conflictEdit)
			}
			worktrees[wt][s.ID] = conflictEdit{id: s.ID, root: root, worktree: wt}
		}
		for _, f := range files[s.ID] {
			if f.Path == "" || f.TS.IsZero() || now.Sub(f.TS) > 6*time.Hour {
				continue
			}
			path := f.Path
			path = pathx.RecordedJoin(s.Cwd, path)
			if !pathx.RecordedIsAbs(path) {
				continue
			}
			path = pathx.RecordedKey(path)
			e := conflictEdit{id: s.ID, root: root, worktree: wt, at: f.TS}
			if absoluteFiles[path] == nil {
				absoluteFiles[path] = make(map[int64]conflictEdit)
			}
			conflictLatest(absoluteFiles[path], e)
			fileRepo, fileWT, rel := "", "", ""
			if worktreeOf != nil {
				fileRepo, fileWT, rel = worktreeOf(path)
			}
			if fileRepo == "" && repo != "" && wt != "" {
				fileRepo, fileWT = repo, wt
				rel, _ = pathx.RecordedRel(wt, path)
			}
			rel = pathx.RecordedKey(rel)
			if fileRepo == "" || fileWT == "" || rel == "." || pathx.RecordedIsAbs(rel) || !pathx.RecordedWithin(rel, ".") {
				continue
			}
			key := conflictFileKey{repo: pathx.RecordedKey(fileRepo), rel: pathx.RecordedKey(rel), branch: s.Branch}
			if otherFiles[key] == nil {
				otherFiles[key] = make(map[int64]conflictEdit)
			}
			e.worktree = pathx.RecordedKey(fileWT)
			conflictLatest(otherFiles[key], e)
		}
	}
	var result []model.Conflict
	for path, edits := range worktrees {
		if len(edits) >= 2 && !conflictFamily(edits) {
			result = append(result, model.Conflict{Kind: "same_worktree", Path: path, Current: true, SessionIDs: conflictIDs(edits)})
		}
	}
	for path, edits := range absoluteFiles {
		if len(edits) >= 2 {
			result = append(result, conflictFile("same_file", path, edits, now))
		}
	}
	for key, edits := range otherFiles {
		wts := make(map[string]bool)
		for _, e := range edits {
			wts[e.worktree] = true
		}
		if len(edits) >= 2 && len(wts) >= 2 {
			result = append(result, conflictFile("same_file_other_worktree", pathx.RecordedKey(pathx.RecordedJoin(key.repo, key.rel)), edits, now))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		// Different branches can produce the same logical path.
		for k := 0; k < len(a.SessionIDs) && k < len(b.SessionIDs); k++ {
			if a.SessionIDs[k] != b.SessionIDs[k] {
				return a.SessionIDs[k] < b.SessionIDs[k]
			}
		}
		return len(a.SessionIDs) < len(b.SessionIDs)
	})
	return result
}

func conflictLatest(edits map[int64]conflictEdit, e conflictEdit) {
	if old, ok := edits[e.id]; !ok || e.at.After(old.at) {
		edits[e.id] = e
	}
}

func conflictRoot(id int64, parents map[int64]int64) int64 {
	seen := make(map[int64]bool)
	for {
		if seen[id] {
			// Malformed cycles still have one deterministic family root.
			root := id
			for p := parents[id]; p != id; p = parents[p] {
				if p < root {
					root = p
				}
			}
			return root
		}
		seen[id] = true
		parent, ok := parents[id]
		if !ok || parent == 0 {
			return id
		}
		id = parent
	}
}

func conflictFamily(edits map[int64]conflictEdit) bool {
	var root int64
	first := true
	for _, e := range edits {
		if !first && root != e.root {
			return false
		}
		root, first = e.root, false
	}
	return true
}

func conflictIDs(edits map[int64]conflictEdit) []int64 {
	ids := make([]int64, 0, len(edits))
	for id := range edits {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func conflictFile(kind, path string, edits map[int64]conflictEdit, now time.Time) model.Conflict {
	var latest, second time.Time
	for _, e := range edits {
		if e.at.After(latest) {
			second, latest = latest, e.at
		} else if e.at.After(second) {
			second = e.at
		}
	}
	return model.Conflict{Kind: kind, Path: path, Current: now.Sub(second) <= 30*time.Minute, Family: conflictFamily(edits), SessionIDs: conflictIDs(edits)}
}
