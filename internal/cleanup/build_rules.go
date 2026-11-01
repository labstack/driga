package cleanup

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

type buildRule struct {
	Dir      string
	Evidence []string
}

var buildRules = []buildRule{
	{"node_modules", []string{"package.json"}},
	{"target", []string{"Cargo.toml"}},
	{".venv", []string{"pyproject.toml", "requirements*.txt", "uv.lock", "setup.py"}},
	{"venv", []string{"pyproject.toml", "requirements*.txt", "uv.lock", "setup.py"}},
	{".gradle", []string{"build.gradle*", "settings.gradle*"}},
	{"build", []string{"build.gradle*", "settings.gradle*"}},
	{".next", []string{"package.json"}},
	{".nuxt", []string{"package.json"}},
	{".svelte-kit", []string{"package.json"}},
	{".turbo", []string{"package.json"}},
	{"dist", []string{"package.json"}},
	{".build", []string{"Package.swift"}},
	{"Pods", []string{"Podfile"}},
	{".pio", []string{"platformio.ini"}},
}

var errNotBuild = errors.New("not a known regenerable directory")

var neverBuild = []string{"out", ".terraform", ".codegraph"}

// regenerableOnly directories may leave with a worktree, but are too small and
// numerous to list as standalone build candidates.
var regenerableOnly = []string{"__pycache__"}

func qualifyRegenerable(checkout, rel string, user []string) (string, error) {
	return qualifyBuild(checkout, rel, slices.Concat(regenerableOnly, user))
}

func qualifyBuild(checkout, rel string, user []string) (rule string, err error) {
	if !strings.HasSuffix(rel, "/") || !filepath.IsLocal(strings.TrimSuffix(rel, "/")) {
		return "", errNotBuild
	}
	dir := filepath.Base(filepath.Clean(rel))
	if slices.Contains(neverBuild, dir) {
		return "", errNotBuild
	}
	var missing error
	for _, rule := range buildRules {
		if rule.Dir != dir {
			continue
		}
		if len(rule.Evidence) == 0 {
			return dir, nil
		}
		for _, glob := range rule.Evidence {
			matches, err := filepath.Glob(filepath.Join(filepath.Dir(filepath.Join(checkout, rel)), glob))
			if err != nil {
				return "", err
			}
			if len(matches) > 0 {
				return dir, nil
			}
		}
		missing = fmt.Errorf("no %s beside %s", rule.Evidence[0], dir)
	}
	for _, name := range user {
		if strings.TrimSuffix(name, "/") == dir {
			return dir, nil
		}
	}
	if missing != nil {
		return "", missing
	}
	return "", errNotBuild
}
