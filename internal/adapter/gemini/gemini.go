// Package gemini reads synthetic-tested Gemini CLI conversation formats.
package gemini

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/tail"
)

const maxBytes int64 = 8 << 20
const maxCachedFiles = 32

type cachedFile struct {
	next model.Offset
	fold conversation
}

type geminiAdapter struct {
	mu    sync.Mutex
	cache map[string]cachedFile
	// Injectable legacy opener; all opens are read-only.
	open func(string) (*os.File, error)
}

// New returns the Gemini CLI adapter.
func New() adapter.Adapter                      { return &geminiAdapter{cache: make(map[string]cachedFile), open: os.Open} }
func (*geminiAdapter) Name() string             { return "gemini" }
func (*geminiAdapter) TestedVersions() []string { return []string{"0.46"} }

func (a *geminiAdapter) Collect(ctx context.Context, env adapter.Env) ([]model.SessionObs, []adapter.Warning, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if env.Offsets == nil {
		return nil, nil, fmt.Errorf("gemini: offset store is required")
	}
	// Gemini's homedir() override replaces HOME, not the .gemini directory.
	home := os.Getenv("GEMINI_CLI_HOME")
	if home == "" {
		home = env.Home
	}
	root := filepath.Join(home, ".gemini", "tmp")
	var paths []string
	for _, pattern := range []string{"*/chats/*.jsonl", "*/chats/*/*.jsonl", "*/chats/session-*.json"} {
		found, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			return nil, nil, err
		}
		paths = append(paths, found...)
	}
	var observations []model.SessionObs
	var warnings []adapter.Warning
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return observations, warnings, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return observations, warnings, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		prev, _, err := env.Offsets.GetOffset(path)
		if err != nil {
			return observations, warnings, err
		}
		now := env.Now
		if now.IsZero() {
			now = time.Now()
		}
		age := now.Sub(info.ModTime())
		if age > 24*time.Hour {
			if env.Baseline && prev.Inode == 0 && age > 7*24*time.Hour {
				end, err := tail.SeekEnd(path)
				if err != nil {
					return observations, warnings, err
				}
				if err := env.Offsets.SetOffset(path, end); err != nil {
					return observations, warnings, err
				}
			}
			delete(a.cache, path)
			continue
		}
		var f conversation
		var next model.Offset
		var w []adapter.Warning
		if strings.HasSuffix(path, ".json") {
			// A migrated JSONL sibling is authoritative; don't alternate old
			// and new snapshots of the same conversation every pass.
			if _, err := os.Stat(path + "l"); err == nil {
				continue
			}
			f, next, w, err = a.readLegacy(ctx, path, prev)
		} else {
			f, next, w, err = a.readJSONL(ctx, path, prev)
		}
		warnings = append(warnings, w...)
		if err != nil {
			return observations, warnings, err
		}
		if err := env.Offsets.SetOffset(path, next); err != nil {
			return observations, warnings, err
		}
		if f.sessionID == "" {
			continue
		}
		project := filepath.Dir(filepath.Dir(path))
		parent := ""
		if filepath.Base(filepath.Dir(path)) != "chats" {
			parent = filepath.Base(filepath.Dir(path))
			project = filepath.Dir(project)
		}
		cwdBytes, err := os.ReadFile(filepath.Join(project, ".project_root"))
		cwd := strings.TrimSpace(string(cwdBytes))
		if err != nil || !filepath.IsAbs(cwd) {
			cwd = ""
		}
		obs := f.observation(path, cwd, env.CaptureSnippets)
		obs.ParentExternalID = parent
		observations = append(observations, obs)
	}
	return observations, warnings, nil
}

func (a *geminiAdapter) remember(path string, c cachedFile) {
	if len(a.cache) >= maxCachedFiles { // Bounded memory even on large histories.
		for key := range a.cache {
			if key != path {
				delete(a.cache, key)
				break
			}
		}
	}
	a.cache[path] = c
}
