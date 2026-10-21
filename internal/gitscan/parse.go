// Package gitscan discovers local repositories and reads their worktree state.
package gitscan

import (
	"strconv"
	"strings"
)

// WorktreeEntry is an entry from git worktree list --porcelain.
type WorktreeEntry struct {
	Path, Head, Branch string
	Bare, Detached     bool
}

// ParseWorktreeList parses porcelain worktree records, ignoring unknown fields.
func ParseWorktreeList(out []byte) []WorktreeEntry {
	var entries []WorktreeEntry
	var current *WorktreeEntry
	for _, line := range strings.Split(string(out), "\n") {
		key, value, _ := strings.Cut(line, " ")
		if key == "worktree" {
			entries = append(entries, WorktreeEntry{Path: unquotePath(value)})
			current = &entries[len(entries)-1]
			continue
		}
		if current == nil {
			continue
		}
		switch key {
		case "HEAD":
			current.Head = value
		case "branch":
			current.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "bare":
			current.Bare = true
		case "detached":
			current.Detached = true
		}
	}
	return entries
}

// Status is the tracked change and branch information in porcelain v2 output.
type Status struct {
	Branch        string
	Ahead, Behind int
	Dirty         int
	DirtyPaths    []string
}

// ParseStatusV2 parses tracked entries, counting each changed path once.
func ParseStatusV2(out []byte) Status {
	var status Status
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "# branch.head ") {
			status.Branch = strings.TrimPrefix(line, "# branch.head ")
			continue
		}
		if strings.HasPrefix(line, "# branch.ab ") {
			fields := strings.Fields(strings.TrimPrefix(line, "# branch.ab "))
			if len(fields) == 2 {
				status.Ahead, _ = strconv.Atoi(strings.TrimPrefix(fields[0], "+"))
				status.Behind, _ = strconv.Atoi(strings.TrimPrefix(fields[1], "-"))
			}
			continue
		}
		n := 0
		switch {
		case strings.HasPrefix(line, "1 "):
			n = 9
		case strings.HasPrefix(line, "2 "):
			n = 10
		case strings.HasPrefix(line, "u "):
			n = 11
		default:
			continue
		}
		fields := strings.SplitN(line, " ", n)
		if len(fields) != n {
			continue
		}
		p := fields[n-1]
		if n == 10 {
			p, _, _ = strings.Cut(p, "\t")
		}
		if p == "" {
			continue
		}
		status.Dirty++
		status.DirtyPaths = append(status.DirtyPaths, unquotePath(p))
	}
	return status
}

func unquotePath(p string) string {
	if strings.HasPrefix(p, "\"") {
		if decoded, err := strconv.Unquote(p); err == nil {
			return decoded
		}
	}
	return p
}
