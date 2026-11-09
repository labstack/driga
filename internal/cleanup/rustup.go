package cleanup

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Toolchains are always report-only. Absence from known repository pins is
// evidence for review, not proof that no shell or unobserved project uses one.
func rustupCandidates(ctx context.Context, e Env, s snapshot) []Candidate {
	home := os.Getenv("RUSTUP_HOME")
	if home == "" {
		home = filepath.Join(e.Home, ".rustup")
	}
	if !filepath.IsAbs(home) {
		return nil
	}
	root := filepath.Join(home, "toolchains")
	aggregate := func() []Candidate {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			return nil
		}
		return []Candidate{{ID: "cache:rustup", Kind: Cache, Path: root, Status: "report_only", Ignored: []IgnoredPath{}, Evidence: []string{"report only; per-toolchain inventory or repository usage unavailable"}}}
	}
	if !e.RunTools {
		return aggregate()
	}
	toolCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	out, err := e.tool(toolCtx, "rustup", "toolchain", "list")
	expired := toolCtx.Err() != nil
	cancel()
	if err != nil || expired || len(out) > 1<<20 {
		return aggregate()
	}
	pins := []string{os.Getenv("RUSTUP_TOOLCHAIN")}
	var settings struct {
		Default   string            `toml:"default_toolchain"`
		Overrides map[string]string `toml:"overrides"`
	}
	data, readErr := readRustupConfig(filepath.Join(home, "settings.toml"))
	complete := readErr == nil || errors.Is(readErr, os.ErrNotExist)
	if readErr == nil {
		complete = toml.Unmarshal(data, &settings) == nil
		pins = append(pins, settings.Default)
		for _, pin := range settings.Overrides {
			pins = append(pins, pin)
		}
	}
	paths := map[string]bool{}
	for _, repo := range s.repos {
		paths[repo.Path] = true
	}
	for _, wt := range s.worktrees {
		paths[wt.Path] = true
	}
	for path := range paths {
		if ctx.Err() != nil {
			return aggregate()
		}
		pin, err := rustupRepositoryPin(path)
		if err != nil {
			complete = false
		} else {
			pins = append(pins, pin)
		}
	}
	rows := []Candidate{}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		if !filepath.IsLocal(name) || strings.ContainsAny(name, `/\`) || name == "." || seen[name] {
			continue
		}
		seen[name] = true
		path := filepath.Join(root, name)
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		used := strings.Contains(line, "(default") || strings.Contains(line, "(active") || strings.Contains(line, ", default") || strings.Contains(line, ", active")
		for _, pin := range pins {
			if pin != "" && (rustupPinMatches(name, pin) || filepath.IsAbs(pin) && resolved(path) == resolved(pin)) {
				used = true
			}
		}
		reason := "not referenced by known repositories or current rustup selections; manual workload review required"
		if !complete {
			reason = "repository usage unavailable; configuration could not be fully inspected"
		}
		if used {
			reason = "referenced by a known repository, default, override or environment selection"
		}
		rows = append(rows, Candidate{ID: fmt.Sprintf("cache:rustup:%x", sha256.Sum256([]byte(path)))[:33], Kind: Cache, Path: path, Status: "report_only", Ignored: []IgnoredPath{}, Evidence: []string{"report only; " + reason}})
	}
	if len(rows) == 0 {
		return aggregate()
	}
	return rows
}

func readRustupConfig(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, linkErr := os.Lstat(path); linkErr == nil {
				return nil, fmt.Errorf("rustup configuration target unavailable")
			}
		}
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("rustup configuration is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 64<<10+1))
	if len(data) > 64<<10 {
		return nil, fmt.Errorf("rustup configuration exceeds inspection limit")
	}
	return data, err
}

func rustupPinMatches(name, pin string) bool {
	if name == pin || strings.HasPrefix(name, pin+"-") {
		return true
	}
	parts := strings.Split(pin, ".")
	if len(parts) == 2 {
		_, majorErr := strconv.ParseUint(parts[0], 10, 64)
		_, minorErr := strconv.ParseUint(parts[1], 10, 64)
		return majorErr == nil && minorErr == nil && strings.HasPrefix(name, pin+".")
	}
	return false
}

// Rustup inherits the closest toolchain file; the legacy filename wins when
// both exist. Directory overrides are conservatively retained independently.
func rustupRepositoryPin(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("repository path unavailable")
	}
	for depth := 0; depth < 64; depth++ {
		for _, name := range []string{"rust-toolchain", "rust-toolchain.toml"} {
			data, err := readRustupConfig(filepath.Join(path, name))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return "", err
			}
			value := strings.TrimSpace(string(data))
			if name == "rust-toolchain" && value != "" && !strings.ContainsAny(value, " \t\r\n[]#=\"'") {
				return value, nil
			}
			var file struct {
				Toolchain struct{ Channel, Path string }
			}
			if err := toml.Unmarshal(data, &file); err != nil || file.Toolchain.Channel == "" && !filepath.IsAbs(file.Toolchain.Path) || file.Toolchain.Channel != "" && file.Toolchain.Path != "" {
				return "", fmt.Errorf("repository toolchain selection unavailable")
			}
			if file.Toolchain.Path != "" {
				return file.Toolchain.Path, nil
			}
			return file.Toolchain.Channel, nil
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", nil
		}
		path = parent
	}
	return "", fmt.Errorf("repository toolchain ancestry exceeds inspection limit")
}
