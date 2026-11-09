package cleanup

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/labstack/driga/internal/platform"
)

const goCacheREADME = "This directory holds cached build artifacts from the Go build system."

func goTmpCacheFingerprint(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	readme := filepath.Join(path, "README")
	info, err = os.Lstat(readme)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	f, err := os.Open(readme)
	if err != nil {
		return false
	}
	defer f.Close()
	prefix := make([]byte, len(goCacheREADME))
	_, err = io.ReadFull(f, prefix)
	return err == nil && string(prefix) == goCacheREADME
}

func goTmpCacheCandidates(ctx context.Context, e Env, s snapshot) []Candidate {
	var cs []Candidate
	seen := map[string]bool{}
	var active string
	located := false
	for _, root := range roots(e) {
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			path := filepath.Join(root, entry.Name())
			if !goTmpCacheFingerprint(path) {
				continue
			}
			path = resolved(path)
			if seen[path] {
				continue
			}
			seen[path] = true
			if !located {
				active, located = configuredGoCache(ctx, e), true
			}
			if path == active {
				continue
			}
			c, err := inspectGoTmpCache(e, s, path)
			if err != nil {
				if !e.Why {
					continue
				}
				c.Status, c.Evidence = "refused", []string{err.Error()}
			}
			cs = append(cs, c)
		}
	}
	return cs
}

func configuredGoCache(ctx context.Context, e Env) string {
	// The service can honor an exported path without running Go or its shims.
	path := os.Getenv("GOCACHE")
	if e.RunTools {
		locateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		out, err := e.tool(locateCtx, "go", "env", "GOCACHE")
		if err != nil || locateCtx.Err() != nil {
			return ""
		}
		path = strings.TrimSpace(string(out))
	}
	if !filepath.IsAbs(path) {
		return ""
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	return canonical
}

func inspectGoTmpCache(e Env, s snapshot, path string) (Candidate, error) {
	c := Candidate{ID: fmt.Sprintf("cache:go-tmp:%x", sha256.Sum256([]byte(path)))[:25], Kind: Cache, Path: path, Status: "ready", Ignored: []IgnoredPath{}}
	attributed := false
	for _, root := range roots(e) {
		if filepath.IsAbs(path) && filepath.Dir(path) == resolved(root) {
			attributed = true
			break
		}
	}
	if !attributed || resolved(path) != path {
		return c, fmt.Errorf("cache must be a direct real directory in a temp root")
	}
	if !goTmpCacheFingerprint(path) {
		return c, fmt.Errorf("not a Go build cache directory")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return c, err
	}
	latest := info.ModTime()
	children, err := os.ReadDir(path)
	if err != nil {
		return c, err
	}
	for _, child := range children {
		info, err := child.Info()
		if err != nil {
			return c, err
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	if e.now().Sub(latest) <= 24*time.Hour {
		return c, fmt.Errorf("Go build cache active within 24h")
	}
	if s.cwdUnder(path) {
		return c, fmt.Errorf("process cwd is inside Go build cache")
	}
	open, err := openUnder(s, path)
	if err != nil {
		return c, err
	}
	if open {
		return c, fmt.Errorf("process has an open file inside Go build cache")
	}
	c.Evidence = []string{"Go build cache in temp, idle since " + latest.UTC().Format(time.RFC3339), "clean with: remove " + path}
	c.identity, err = captureIdentity(path)
	return c, err
}

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
	if !e.RunTools {
		return nil, fmt.Errorf("tool commands run from the driga CLI only")
	}
	if e.Tool != nil {
		return e.Tool(ctx, binary, args...)
	}
	return platform.ChildOutput(exec.CommandContext(ctx, binary, args...), false)
}

// directCacheIdle gates deletion by Driga; native tool commands retain their
// own cleanup semantics. Listing and verification each supply a fresh snapshot.
func directCacheIdle(e Env, s snapshot, path string) error {
	path = resolved(path)
	if s.cwdUnder(path) {
		return fmt.Errorf("process cwd is inside cache")
	}
	for session := range s.sessionsUnder(path) {
		if sessionLive(e, session) {
			return fmt.Errorf("live session uses cache")
		}
	}
	open, err := openUnder(s, path)
	if err != nil {
		return err
	}
	if open {
		return fmt.Errorf("process has an open file inside cache")
	}
	return nil
}

func cacheCandidates(ctx context.Context, e Env, s snapshot) []Candidate {
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
				cs = append(cs, rustupCandidates(ctx, e, s)...)
				continue
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
			if err := directCacheIdle(e, s, path); err != nil {
				c.Status, c.Evidence = "refused", []string{err.Error()}
				cs = append(cs, c)
				continue
			}
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
	if strings.HasPrefix(c.ID, "cache:go-tmp:") {
		r.Commands = [][]string{{"remove", c.Path}}
		if opts.DryRun {
			return r, nil
		}
		parent := filepath.Dir(c.Path)
		before, err := freeBytes(parent)
		if err != nil {
			return r, err
		}
		if err := removeCacheDirectory(c.Path); err != nil {
			return r, err
		}
		after, err := freeBytes(parent)
		if err != nil {
			return r, err
		}
		r.FreedBytes = max(0, after-before)
		return r, nil
	}
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
