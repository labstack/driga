package cleanup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/labstack/driga/internal/platform"
)

type cacheTool struct {
	ID, Binary string
	Locate     []string
	Clean      []string
	ReportOnly bool
}

var cacheTools = []cacheTool{
	{"go-build", "go", []string{"env", "GOCACHE"}, []string{"clean", "-cache"}, false},
	{"go-mod", "go", []string{"env", "GOMODCACHE"}, nil, true},
	{"uv", "uv", []string{"cache", "dir"}, []string{"cache", "prune"}, false},
	{"npm", "npm", []string{"config", "get", "cache"}, []string{"cache", "clean", "--force"}, false},
	{"pnpm", "pnpm", []string{"store", "path"}, []string{"store", "prune"}, false},
	{ID: "bun"},
	{"homebrew", "brew", []string{"--cache"}, []string{"cleanup"}, false},
	{ID: "xcode"},
	{ID: "cargo-registry", ReportOnly: true},
	{ID: "rustup", ReportOnly: true},
	{ID: "huggingface", ReportOnly: true},
	{ID: "colima", ReportOnly: true},
	{ID: "docker", ReportOnly: true},
	{ID: "codex", ReportOnly: true},
}

func (e Env) tool(ctx context.Context, binary string, args ...string) ([]byte, error) {
	if e.Tool != nil {
		return e.Tool(ctx, binary, args...)
	}
	return platform.ChildOutput(exec.CommandContext(ctx, binary, args...), false)
}

func cacheCandidates(ctx context.Context, e Env) []Candidate {
	var cs []Candidate
	for _, tool := range cacheTools {
		var path, reason string
		if tool.Binary != "" {
			if !e.RunTools {
				continue
			}
			locateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			out, err := e.tool(locateCtx, tool.Binary, tool.Locate...)
			expired := locateCtx.Err() != nil
			cancel()
			if err != nil || expired {
				continue
			}
			for _, line := range strings.Split(string(out), "\n") {
				if line = strings.TrimSpace(line); line != "" {
					path = line
					break
				}
			}
			reason = "dependency downloads"
		} else {
			switch tool.ID {
			case "bun":
				path = os.Getenv("BUN_INSTALL_CACHE_DIR")
				if path == "" {
					install := os.Getenv("BUN_INSTALL")
					if install == "" {
						install = filepath.Join(e.Home, ".bun")
					}
					path = filepath.Join(install, "install", "cache")
				}
			case "xcode":
				if runtime.GOOS != "darwin" {
					continue
				}
				path = filepath.Join(e.Home, "Library/Developer/Xcode/DerivedData")
			case "cargo-registry":
				path, reason = filepath.Join(e.Home, ".cargo/registry"), "dependency downloads"
			case "rustup":
				path, reason = filepath.Join(e.Home, ".rustup/toolchains"), "toolchain usage by repo is not checked"
			case "huggingface":
				path, reason = filepath.Join(e.Home, ".cache/huggingface"), "model cache"
			case "colima":
				path, reason = filepath.Join(e.Home, ".colima"), "VM disks"
			case "docker":
				path, reason = filepath.Join(e.Home, "Library/Containers/com.docker.docker/Data"), "VM disks"
			case "codex":
				path, reason = filepath.Join(e.Home, ".codex"), "agent history"
			}
		}
		if !filepath.IsAbs(path) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		c := Candidate{ID: "cache:" + tool.ID, Kind: Cache, Path: path, Status: "ready", Ignored: []IgnoredPath{}}
		if tool.ReportOnly {
			c.Status, c.Evidence = "report_only", []string{"report only; " + reason}
		} else if tool.Binary == "" {
			c.Evidence = []string{"clean with: remove " + path}
		} else {
			c.Evidence = []string{"clean with: " + strings.Join(append([]string{tool.Binary}, tool.Clean...), " ")}
		}
		c.identity, err = captureIdentity(path)
		if err != nil {
			continue
		}
		cs = append(cs, c)
	}
	return cs
}

func applyCache(ctx context.Context, e Env, c Candidate, opts ApplyOpts, r Result) (Result, error) {
	for _, tool := range cacheTools {
		if c.ID != "cache:"+tool.ID {
			continue
		}
		if tool.ReportOnly {
			return r, fmt.Errorf("report only")
		}
		if !e.RunTools {
			return r, fmt.Errorf("cache cleanup runs from the driga CLI only")
		}
		command := append([]string{tool.Binary}, tool.Clean...)
		if tool.Binary == "" {
			command = []string{"remove", c.Path}
		}
		r.Commands = [][]string{command}
		if opts.DryRun {
			return r, nil
		}
		parent := filepath.Dir(c.Path)
		before, err := freeBytes(parent)
		if err != nil {
			return r, err
		}
		if tool.Binary == "" {
			if err := removeCacheDirectory(c.Path); err != nil {
				return r, err
			}
		} else if _, err := e.tool(ctx, tool.Binary, tool.Clean...); err != nil {
			return r, err
		}
		after, err := freeBytes(parent)
		if err != nil {
			return r, err
		}
		r.FreedBytes = max(0, after-before)
		return r, nil
	}
	return r, fmt.Errorf("unknown cache tool")
}

func removeCacheDirectory(path string) error {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	return root.RemoveAll(filepath.Base(path))
}
