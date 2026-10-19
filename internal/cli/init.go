package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/hooks"
)

// SuggestRoots returns conventional root directories containing a repo at depth <=3.
// Git worktree .git files count as repositories; symlinks are not traversed.
func SuggestRoots(home string) []string {
	var roots []string
	var seen []os.FileInfo
	for _, name := range []string{"code", "src", "dev", "Projects", "projects", "work", "git"} {
		root := filepath.Join(home, name)
		// A case-insensitive disk resolves Projects and projects to one directory.
		if info, err := os.Stat(root); err == nil {
			if slices.ContainsFunc(seen, func(s os.FileInfo) bool { return os.SameFile(s, info) }) {
				continue
			}
			seen = append(seen, info)
		}
		found := false
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				if entry.Name() == "node_modules" || entry.Name() == ".tmp" {
					return filepath.SkipDir
				}
				rel, err := filepath.Rel(root, path)
				if err != nil {
					return filepath.SkipDir
				}
				depth := 0
				if rel != "." {
					depth = len(strings.Split(rel, string(filepath.Separator)))
				}
				if depth > 3 {
					return filepath.SkipDir
				}
				if info, err := os.Lstat(filepath.Join(path, ".git")); err == nil && (info.IsDir() || info.Mode().IsRegular()) {
					found = true
					return fs.SkipAll
				}
			}
			return nil
		})
		if found {
			roots = append(roots, "~/"+name)
		}
	}
	return roots
}

// InitCmd guides optional setup; every prompt defaults to No.
type InitCmd struct{}

// Run explains and previews changes before writing configuration or agent settings.
func (InitCmd) Run(g *Globals) error {
	input := g.Stdin
	if input == nil {
		input = strings.NewReader("")
	}
	// Pass the same buffered reader to hook installation so reads cannot consume
	// answers intended for a later prompt.
	r := bufio.NewReader(input)
	local := *g
	local.Stdin = r
	out := commandOutput(g)
	ask := func(prompt string) (bool, error) {
		if _, err := fmt.Fprint(out, prompt+" [y/N] "); err != nil {
			return false, err
		}
		line, err := r.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		return answer == "y" || answer == "yes", nil
	}
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	chosen := map[string]bool{}
	roots := SuggestRoots(config.ExpandHome("~"))
	fmt.Fprintln(out, "Roots add repository scanning alongside discovered session working directories.")
	if len(roots) == 0 {
		fmt.Fprintln(out, "No conventional directories containing repositories found.")
	} else {
		fmt.Fprintln(out, "Suggested roots:", strings.Join(roots, ", "))
	}
	yes, err := ask("Use suggested roots?")
	if err != nil {
		return err
	}
	if yes && len(roots) > 0 {
		cfg.Roots = roots
		chosen["roots"] = true
	}
	fmt.Fprintln(out, "Text capture defaults to titles and file paths. Snippets also store scrubbed prompt/handoff text (up to 200 characters); scrubbing cannot guarantee privacy.")
	yes, err = ask("Enable capture_text = snippets?")
	if err != nil {
		return err
	}
	if yes {
		cfg.CaptureText = "snippets"
		chosen["capture_text"] = true
	}
	fmt.Fprintln(out, "Configuration writes commented defaults and selected values to", config.Path())
	yes, err = ask("Write configuration?")
	if err != nil {
		return err
	}
	if yes {
		text := configText(cfg, true, chosen)
		before, readErr := os.ReadFile(config.Path())
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
		write := true
		if readErr == nil {
			diff := hooks.Diff{Path: config.Path(), Before: string(before), After: text, Changed: string(before) != text}
			if _, err := io.WriteString(out, diff.Unified()); err != nil {
				return err
			}
			write, err = ask("Overwrite?")
			if err != nil {
				return err
			}
			// Avoid replacing edits made while the preview was being reviewed.
			if write {
				current, err := os.ReadFile(config.Path())
				if err != nil {
					return err
				}
				if string(current) != string(before) {
					return errors.New("config changed since preview; rerun driga init")
				}
			}
		}
		if write {
			if err := writeConfig(config.Path(), text); err != nil {
				return err
			}
		}
	}
	fmt.Fprintln(out, "Service installation creates a per-user LaunchAgent and starts background collection for inbox and history. Logs: ~/Library/Logs/driga/collector.log. Undo: driga service uninstall.")
	yes, err = ask("Install and start the collector service?")
	if err != nil {
		return err
	}
	if yes {
		if err := (ServiceCmd{Action: "install"}).Run(&local); err != nil {
			return err
		}
	}
	fmt.Fprintln(out, "Hooks merge event commands into ~/.claude/settings.json and an unused notify entry in the Codex config, backing up existing files. You will see a diff before applying. Undo: driga hooks uninstall.")
	yes, err = ask("Preview hook installation?")
	if err != nil {
		return err
	}
	if yes {
		return (HooksInstallCmd{HooksOptions: HooksOptions{Agent: "all"}}).Run(&local)
	}
	return nil
}
