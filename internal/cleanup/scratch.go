package cleanup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
)

// projectSlug mirrors the observed Claude Code 2.1.303 directory encoding:
// replace each non-ASCII-alphanumeric UTF-16 unit with '-', and above 200 units
// append '-' plus abs(signed 32-bit 31*h+unit hash of the original path) in base 36
// to the first 200 encoded characters. JS's non-Unicode regex splits surrogates.
func projectSlug(cwd string) string {
	units := utf16.Encode([]rune(cwd))
	slug := make([]byte, len(units))
	var hash int32
	for i, unit := range units {
		hash = hash*31 + int32(unit)
		slug[i] = '-'
		if unit >= 'a' && unit <= 'z' || unit >= 'A' && unit <= 'Z' || unit >= '0' && unit <= '9' {
			slug[i] = byte(unit)
		}
	}
	if len(slug) <= 200 {
		return string(slug)
	}
	h := int64(hash) // Widen before abs: Math.abs(-2147483648) is positive in JS.
	if h < 0 {
		h = -h
	}
	return string(slug[:200]) + "-" + strconv.FormatInt(h, 36)
}
func validSessionID(id string) bool {
	return id != "" && id != "." && !strings.Contains(id, "..") && !strings.ContainsAny(id, "/\\\x00")
}
func roots(e Env) []string {
	if e.TempRoots != nil {
		return e.TempRoots
	}
	if runtime.GOOS == "windows" {
		return []string{os.TempDir()}
	}
	return []string{"/tmp", "/private/tmp", os.TempDir()}
}
func scratchCandidates(ctx context.Context, e Env, s snapshot) []Candidate {
	if runtime.GOOS == "windows" {
		return nil
	}
	cs := []Candidate{}
	seen := map[string]bool{}
	for _, session := range s.sessions {
		if session.Adapter != "claude" || !validSessionID(session.ExternalID) || !filepath.IsAbs(session.Cwd) {
			continue
		}
		for _, tmp := range roots(e) {
			root := filepath.Join(tmp, fmt.Sprintf("claude-%d", os.Getuid()), projectSlug(session.Cwd))
			path := filepath.Join(root, session.ExternalID)
			canonical, err := filepath.EvalSymlinks(path)
			if err != nil || seen[canonical] {
				continue
			}
			c := Candidate{ID: "scratch:" + session.ExternalID, Kind: Scratch, Path: path, root: root}
			if fresh, err := inspectScratchSession(ctx, e, s, c, session); err == nil {
				cs = append(cs, fresh)
				seen[canonical] = true
			}
		}
	}
	return cs
}
func inspectScratch(ctx context.Context, e Env, s snapshot, c Candidate) (Candidate, error) {
	if runtime.GOOS == "windows" {
		return Candidate{}, fmt.Errorf("Claude scratch cleanup refused: Windows scratch layout is unverified; open-file inspection is unavailable")
	}
	id := strings.TrimPrefix(c.ID, "scratch:")
	if !validSessionID(id) {
		return c, fmt.Errorf("invalid session id")
	}
	for _, session := range s.sessions {
		if session.Adapter == "claude" && session.ExternalID == id {
			for _, tmp := range roots(e) {
				root := filepath.Join(tmp, fmt.Sprintf("claude-%d", os.Getuid()), projectSlug(session.Cwd))
				if c.Path == filepath.Join(root, id) && c.root == root {
					return inspectScratchSession(ctx, e, s, c, session)
				}
			}
		}
	}
	return c, fmt.Errorf("scratch path does not match a stored session and attributed root")
}
func inspectScratchSession(ctx context.Context, e Env, s snapshot, c Candidate, session model.Session) (Candidate, error) {
	if !validSessionID(session.ExternalID) || session.Label != "dead" && session.Label != "finished" {
		return c, fmt.Errorf("owning session is live or invalid")
	}
	// "finished" also labels an unacknowledged completion with its process
	// still running; only an ended session or a gone process releases scratch.
	ended := session.EndedAt != nil || session.PID != 0 && !e.Plat.Alive(session.PID, session.ProcStart)
	if !ended {
		return c, fmt.Errorf("owning session's process is still running")
	}
	paths := map[string]bool{}
	for _, tmp := range roots(e) {
		path := filepath.Join(tmp, fmt.Sprintf("claude-%d", os.Getuid()), projectSlug(session.Cwd), session.ExternalID)
		if info, err := os.Lstat(path); err == nil && info.IsDir() {
			if canonical, err := filepath.EvalSymlinks(path); err == nil {
				paths[canonical] = true
			}
		}
	}
	if len(paths) != 1 {
		return c, fmt.Errorf("scratch attribution is missing or ambiguous across temp roots")
	}
	latest := session.LastActivityAt
	if session.EndedAt != nil && session.EndedAt.After(latest) {
		latest = *session.EndedAt
	}
	if latest.IsZero() || e.now().Sub(latest) < 24*time.Hour {
		return c, fmt.Errorf("owning session ended less than 24h ago")
	}
	root, err := filepath.EvalSymlinks(c.root)
	if err != nil {
		return c, err
	}
	path, err := filepath.EvalSymlinks(c.Path)
	if err != nil {
		return c, err
	}
	// A symlink in the attributed root itself cannot broaden its authority.
	tmpRoot, err := filepath.EvalSymlinks(filepath.Dir(filepath.Dir(c.root)))
	if err != nil {
		return c, err
	}
	expectedRoot := filepath.Join(tmpRoot, fmt.Sprintf("claude-%d", os.Getuid()), projectSlug(session.Cwd))
	if root != expectedRoot || !inside(root, path) || path == root || path != filepath.Join(root, session.ExternalID) {
		return c, fmt.Errorf("scratch symlink escapes attributed root")
	}
	c.rootCanonical = root
	c.rootIdentity, err = os.Stat(root)
	if err != nil {
		return c, err
	}
	info, err := os.Lstat(c.Path)
	if err != nil {
		return c, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return c, fmt.Errorf("scratch must be a real directory")
	}
	for _, cwd := range s.cwds {
		if inside(path, resolved(cwd)) {
			return c, fmt.Errorf("process cwd inside scratch")
		}
	}
	probe, ok := e.Plat.(platform.FileProber)
	if !ok {
		return c, fmt.Errorf("open-file inspection unavailable")
	}
	open, err := probe.OpenFiles(ctx, path)
	if err != nil {
		return c, err
	}
	if open {
		return c, fmt.Errorf("process has an open file inside scratch")
	}
	c.identity = info
	c.Status = "ready"
	c.Evidence = []string{"stored Claude session " + session.ExternalID + " ended at " + latest.UTC().Format(time.RFC3339), "inside attributed temp root; no open files or process cwd"}
	return c, nil
}
