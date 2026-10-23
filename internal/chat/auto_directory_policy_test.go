package chat

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitProject(t *testing.T, dir string) {
	t.Helper()
	if out, err := exec.Command("git", "init", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
}

func TestAutoRequiresSafeProjectDirectory(t *testing.T) {
	root := t.TempDir()
	home, data := filepath.Join(root, "home"), filepath.Join(root, "state", "data")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, path := range []string{home, data, filepath.Join(root, "state"), filepath.Join(home, "project", "src")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	gitProject(t, root)
	for _, dir := range []string{root, home, data, filepath.Dir(data)} {
		if autoAllowed("Write", json.RawMessage(`{"file_path":"ordinary.txt"}`), dir, autoProject(dir, data)) {
			t.Errorf("broad working directory allowed: %s", dir)
		}
	}
	plain := t.TempDir()
	if autoAllowed("Edit", json.RawMessage(`{"file_path":"ordinary.txt"}`), plain, autoProject(plain, data)) {
		t.Fatal("non-project directory allowed")
	}
	// Git discovery must ignore inherited overrides, and accept project subdirs.
	t.Setenv("GIT_DIR", filepath.Join(root, ".git"))
	t.Setenv("GIT_WORK_TREE", plain)
	if autoAllowed("Write", json.RawMessage(`{"file_path":"ordinary.txt"}`), plain, autoProject(plain, data)) {
		t.Fatal("Git environment made a non-project directory a project")
	}
	for _, dir := range []string{filepath.Join(home, "project"), filepath.Join(home, "project", "src")} {
		if !autoAllowed("Write", json.RawMessage(`{"file_path":"ordinary.txt"}`), dir, autoProject(dir, data)) {
			t.Errorf("project working directory refused: %s", dir)
		}
	}
}

func TestAutoHiddenAndInstructionComponents(t *testing.T) {
	dir, data := t.TempDir(), t.TempDir()
	gitProject(t, dir)
	for _, path := range []string{
		"CLAUDE.local.md", "sub/AGENTS.override.md", "Gemini-team.MD", "sub/COPILOT-instructions.md",
		".cursorrules", ".windsurfrules", ".cursor/tasks.json", ".zed/settings.json", "sub/.new-agent/config",
		"CLAUDE.local.md/file", "sub/agents-extra.md/file", ".mcp.json", ".envrc",
	} {
		for _, tool := range []string{"Edit", "Write"} {
			raw, _ := json.Marshal(map[string]string{"file_path": path})
			if autoAllowed(tool, raw, dir, autoProject(dir, data)) {
				t.Errorf("%s allowed %s", tool, path)
			}
		}
	}
	protected := filepath.Join(dir, "CLAUDE.local.md")
	if err := os.WriteFile(protected, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(protected, filepath.Join(dir, "ordinary.md")); err != nil {
		t.Fatal(err)
	}
	if autoAllowed("Edit", json.RawMessage(`{"file_path":"ordinary.md"}`), dir, autoProject(dir, data)) {
		t.Fatal("resolved instruction file allowed through symlink")
	}
}

func TestAutoWindowsNormalizedComponents(t *testing.T) {
	dir, data := t.TempDir(), t.TempDir()
	gitProject(t, dir)
	for _, path := range []string{
		`.git.\hooks\pre-commit`, `.git \config`, `src\CLAUDE.local.md. `,
		`CLAUDE.md::$DATA`, `file:stream`, `src/ordinary.txt:stream`, `sub/AGENTS.override.md. `,
		`src/.cursor. /settings`, `src/Copilot-Instructions.md `,
	} {
		raw, _ := json.Marshal(map[string]string{"file_path": path})
		if autoAllowed("Write", raw, dir, autoProject(dir, data)) {
			t.Errorf("Windows alias allowed: %q", path)
		}
	}
}
