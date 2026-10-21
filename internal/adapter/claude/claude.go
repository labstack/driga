// Package claude collects Claude Code registry and transcript metadata.
package claude

import (
	"context"
	"errors"
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

const (
	discoveryWindow = 24 * time.Hour
	baselineAge     = 7 * 24 * time.Hour
)

type claudeAdapter struct{}

// New returns a Claude Code adapter. Folded session state stays in the collector.
func New() adapter.Adapter { return claudeAdapter{} }

func (claudeAdapter) Name() string { return "claude" }

func (claudeAdapter) TestedVersions() []string { return []string{"2.1"} }

func (a claudeAdapter) Collect(ctx context.Context, env adapter.Env) ([]model.SessionObs, []adapter.Warning, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	home := env.Home
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, nil, err
		}
	}
	home, err := filepath.Abs(home)
	if err != nil {
		return nil, nil, err
	}
	if env.Now.IsZero() {
		env.Now = time.Now()
	}
	claudeHome := os.Getenv("CLAUDE_CONFIG_DIR")
	if claudeHome == "" {
		claudeHome = filepath.Join(home, ".claude")
	}
	registryPaths, err := filepath.Glob(filepath.Join(claudeHome, "sessions", "*.json"))
	if err != nil {
		return nil, nil, err
	}
	var observations []model.SessionObs
	var warnings []adapter.Warning
	indices := make(map[string]int)
	registered := make(map[string]bool)
	warnedVersions := make(map[string]bool)
	for _, path := range registryPaths {
		if err := ctx.Err(); err != nil {
			return nil, warnings, err
		}
		o, err := readRegistry(path, env)
		if err != nil {
			warnings = append(warnings, adapter.Warning{Kind: "bad_registry", Detail: fmt.Sprintf("%s: %v", path, err)})
			continue
		}
		if !a.testedVersion(o.Version) && !warnedVersions[o.Version] {
			warnings = append(warnings, adapter.Warning{Kind: "untested_version", Detail: o.Version})
			warnedVersions[o.Version] = true
		}
		registered[o.ExternalID] = true
		if i, ok := indices[o.ExternalID]; ok {
			observations[i] = o
		} else {
			indices[o.ExternalID] = len(observations)
			observations = append(observations, o)
		}
	}
	transcripts, err := filepath.Glob(filepath.Join(claudeHome, "projects", "*", "*.jsonl"))
	if err != nil {
		return nil, warnings, err
	}
	type task struct {
		index   int
		paths   []string
		offsets *parseOffsets
	}
	var tasks []task
	taskByIndex := make(map[int]int)
	for _, path := range transcripts {
		if err := ctx.Err(); err != nil {
			return nil, warnings, err
		}
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, warnings, fmt.Errorf("stat transcript %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		sid := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		if env.Baseline && info.ModTime().Before(env.Now.Add(-baselineAge)) {
			o, err := tail.SeekEnd(path)
			if err != nil {
				return nil, warnings, fmt.Errorf("baseline %s: %w", path, err)
			}
			if env.Offsets != nil {
				if err := env.Offsets.SetOffset(path, o); err != nil {
					return nil, warnings, fmt.Errorf("set offset %s: %w", path, err)
				}
			}
			if registered[sid] {
				observations[indices[sid]].SourcePath = path
			}
			continue
		}
		if !registered[sid] && info.ModTime().Before(env.Now.Add(-discoveryWindow)) {
			continue
		}
		i, ok := indices[sid]
		if !ok {
			i = len(observations)
			indices[sid] = i
			observations = append(observations, model.SessionObs{Adapter: a.Name(), ExternalID: sid, Origin: "interactive"})
		}
		j, exists := taskByIndex[i]
		if !exists {
			j = len(tasks)
			taskByIndex[i] = j
			tasks = append(tasks, task{index: i, offsets: &parseOffsets{prev: make(map[string]model.Offset), next: make(map[string]model.Offset)}})
		}
		tasks[j].paths = append(tasks[j].paths, path)
		if env.Offsets != nil {
			prev, _, err := env.Offsets.GetOffset(path)
			if err != nil {
				return nil, warnings, fmt.Errorf("get offset %s: %w", path, err)
			}
			tasks[j].offsets.prev[path] = prev
		}
	}
	type result struct {
		obs      model.SessionObs
		warnings []adapter.Warning
		err      error
	}
	results := make([]result, len(tasks))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(4, len(tasks)) {
		workers.Go(func() {
			for j := range jobs {
				t := tasks[j]
				results[j].obs = observations[t.index]
				localEnv := env
				localEnv.Offsets = t.offsets
				for _, path := range t.paths {
					if err := ctx.Err(); err != nil {
						results[j].err = err
						break
					}
					results[j].obs.SourcePath = path
					ws, err := tailTranscript(ctx, path, &results[j].obs, localEnv)
					results[j].warnings = append(results[j].warnings, ws...)
					if err != nil {
						results[j].err = err
						break
					}
				}
			}
		})
	}
	for j := range tasks {
		jobs <- j
	}
	close(jobs)
	workers.Wait()
	for j, result := range results {
		warnings = append(warnings, result.warnings...)
		if result.err != nil {
			return nil, warnings, result.err
		}
		observations[tasks[j].index] = result.obs
		if env.Offsets != nil {
			for _, path := range tasks[j].paths {
				if next, ok := tasks[j].offsets.next[path]; ok {
					if err := env.Offsets.SetOffset(path, next); err != nil {
						return nil, warnings, fmt.Errorf("set offset %s: %w", path, err)
					}
				}
			}
		}
	}

	for i := range observations {
		if temporaryCwd(observations[i].Cwd) {
			observations[i].Origin += "/temporary"
		}
	}
	return observations, warnings, nil
}

func (a claudeAdapter) testedVersion(version string) bool {
	for _, tested := range a.TestedVersions() {
		if version == tested || strings.HasPrefix(version, tested+".") {
			return true
		}
	}
	return false
}

// parseOffsets isolates per-session tail writes from the caller's offset store.
type parseOffsets struct{ prev, next map[string]model.Offset }

func (o *parseOffsets) GetOffset(path string) (model.Offset, bool, error) {
	if v, ok := o.next[path]; ok {
		return v, true, nil
	}
	v, ok := o.prev[path]
	return v, ok, nil
}
func (o *parseOffsets) SetOffset(path string, v model.Offset) error { o.next[path] = v; return nil }
