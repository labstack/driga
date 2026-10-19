// Package codex collects Codex rollout observations and catalog titles.
package codex

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

var maxBytes int64 = 8 << 20

type codexAdapter struct{}

// New returns an adapter for the tested Codex rollout format.
func New() adapter.Adapter { return codexAdapter{} }

func (codexAdapter) Name() string             { return "codex" }
func (codexAdapter) TestedVersions() []string { return []string{"0.16"} }

func (a codexAdapter) Collect(ctx context.Context, env adapter.Env) ([]model.SessionObs, []adapter.Warning, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if env.Offsets == nil {
		return nil, nil, fmt.Errorf("codex: offset store is required")
	}
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		home = filepath.Join(env.Home, ".codex")
	}
	db, warnings := openCatalog(ctx, filepath.Join(home, "codex-dev.db"))
	if db != nil {
		defer db.Close()
	}
	paths, err := filepath.Glob(filepath.Join(home, "sessions", "*", "*", "*", "rollout-*.jsonl"))
	if err != nil {
		return nil, warnings, err
	}
	now := env.Now
	if now.IsZero() {
		now = time.Now()
	}
	type task struct {
		path string
		prev model.Offset
	}
	var tasks []task
	var observations []model.SessionObs
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
		age := now.Sub(info.ModTime())
		if age > 24*time.Hour && (!env.Baseline || age <= 7*24*time.Hour) {
			continue
		}
		prev, exists, err := env.Offsets.GetOffset(path)
		if err != nil {
			return observations, warnings, err
		}
		if env.Baseline && !exists && now.Sub(info.ModTime()) > 7*24*time.Hour {
			end, err := tail.SeekEnd(path)
			if err != nil {
				return observations, warnings, err
			}
			if err := env.Offsets.SetOffset(path, end); err != nil {
				return observations, warnings, err
			}
			continue
		}
		if now.Sub(info.ModTime()) > 24*time.Hour {
			continue
		}
		tasks = append(tasks, task{path: path, prev: prev})
	}
	type result struct {
		obs      model.SessionObs
		warnings []adapter.Warning
		next     model.Offset
		err      error
	}
	results := make([]result, len(tasks))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(4, len(tasks)) {
		workers.Go(func() {
			for i := range jobs {
				if err := ctx.Err(); err != nil {
					results[i].err = err
					continue
				}
				t := tasks[i]
				results[i].obs, results[i].warnings, results[i].next, results[i].err = readRolloutContext(ctx, t.path, t.prev, env.CaptureSnippets)
			}
		})
	}
	for i := range tasks {
		jobs <- i
	}
	close(jobs)
	workers.Wait()
	for i, result := range results {
		path := tasks[i].path
		obs, next := result.obs, result.next
		warnings = append(warnings, result.warnings...)
		if result.err != nil {
			return observations, warnings, result.err
		}
		if obs.Version != "" {
			tested := false
			for _, prefix := range a.TestedVersions() {
				tested = tested || strings.HasPrefix(obs.Version, prefix)
			}
			if !tested {
				warnings = append(warnings, adapter.Warning{Kind: "untested_version", Detail: path + ": " + obs.Version})
			}
		}
		if db != nil {
			title, err := catalogTitle(ctx, db, obs.ExternalID)
			if err != nil {
				warnings = append(warnings, adapter.Warning{Kind: "catalog_unavailable", Detail: err.Error()})
				// Report one catalog failure per collection, even for many rollouts.
				db.Close()
				db = nil
			} else {
				obs.Title = title
			}
		}
		if err := env.Offsets.SetOffset(path, next); err != nil {
			return observations, warnings, err
		}
		observations = append(observations, obs)
	}
	return observations, warnings, nil
}
