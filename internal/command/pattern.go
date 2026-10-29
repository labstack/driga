package command

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
)

// ArgvPattern has literal positions and, only for a recognized grammar, a
// canonical descendant path slot. It is constructed by the broker, never input.
type ArgvPattern struct {
	Family         Class          `json:"family"`
	Argv           []string       `json:"argv"`
	PathPositions  map[int]string `json:"path_positions,omitempty"`
	Cwd            string         `json:"cwd"`
	Project        string         `json:"project"`
	Scratch        string         `json:"scratch"`
	Executable     string         `json:"executable"`
	ExecutableHash [32]byte       `json:"-"`
	TimeoutSeconds int            `json:"timeout_seconds"`
	WritableRoots  []string       `json:"writable_roots,omitempty"`
}

func NewArgvPattern(p Prepared) (ArgvPattern, error) {
	if p.Policy.RequiresCard {
		return ArgvPattern{}, errors.New("read outside scope requires individual approval")
	}
	r := p.Parsed.Request
	if (p.Policy.Class != Read && p.Policy.Class != Other) || r.Network || r.Script != "" || len(r.Argv) == 0 || len(p.Policy.Commands) != 1 || (!p.Sandbox.Available && p.Policy.Class != Read) || p.Parsed.Complex || p.Parsed.Pipeline {
		return ArgvPattern{}, errors.New("command is not eligible for a grant")
	}
	cwd, e := filepath.EvalSymlinks(r.Cwd)
	if e != nil || cwd != r.Cwd {
		return ArgvPattern{}, errors.New("grant cwd must be canonical")
	}
	pat := ArgvPattern{Family: p.Policy.Class, Argv: append([]string(nil), r.Argv...), Cwd: cwd, Project: p.Scope.Project, Scratch: p.Scope.Scratch, Executable: p.Policy.Commands[0].Path, ExecutableHash: p.Policy.Commands[0].SHA256, TimeoutSeconds: r.TimeoutSeconds}
	pat.WritableRoots = slices.Clone(p.WritableRoots)
	if len(r.Argv) == 4 && filepath.Base(r.Argv[0]) == "git" && r.Argv[1] == "worktree" && r.Argv[2] == "remove" && p.Scope.Project != "" {
		pat.PathPositions = map[int]string{3: p.Scope.Project}
		if !boundedPatternPath(p.Scope.Project, cwd, r.Argv[3]) {
			return ArgvPattern{}, errors.New("path is outside the grant project")
		}
	}
	return pat, nil
}
func boundedPatternPath(root, cwd, path string) bool {
	if path == "" || strings.HasPrefix(path, "-") {
		return false
	}
	for _, part := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return false
		}
	}
	canonicalRoot, e := filepath.EvalSymlinks(root)
	if e != nil || canonicalRoot != root {
		return false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	// This grammar removes existing worktrees. Require complete resolution;
	// ascending past a dangling symlink would mistake its outside target for a
	// missing literal child under the project.
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, resolved)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func MatchPattern(pattern ArgvPattern, p Prepared) bool {
	candidate, err := NewArgvPattern(p)
	if !slices.Equal(candidate.WritableRoots, pattern.WritableRoots) {
		return false
	}
	if err != nil || candidate.Family != pattern.Family || candidate.Cwd != pattern.Cwd || candidate.Project != pattern.Project || candidate.Scratch != pattern.Scratch || candidate.Executable != pattern.Executable || candidate.ExecutableHash != pattern.ExecutableHash || candidate.TimeoutSeconds != pattern.TimeoutSeconds || len(candidate.Argv) != len(pattern.Argv) {
		return false
	}
	for i, arg := range pattern.Argv {
		if root, ok := pattern.PathPositions[i]; ok {
			if !boundedPatternPath(root, candidate.Cwd, candidate.Argv[i]) {
				return false
			}
		} else if candidate.Argv[i] != arg {
			return false
		}
	}
	return true
}
func (p ArgvPattern) String() string {
	args := slices.Clone(p.Argv)
	for i, root := range p.PathPositions {
		args[i] = "<path under " + root + ">"
	}
	raw, _ := json.Marshal(args)
	return string(p.Family) + " · " + string(raw) + " · in " + p.Cwd
}
