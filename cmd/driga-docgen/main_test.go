// The reference pages document the macOS and Linux hook commands; on Windows
// the installers write Windows paths, so the pages are generated and checked
// on Unix only (`just docs`, `just docs-check`, CI's Unix job).

//go:build !windows

package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"

	"github.com/labstack/driga/internal/config"
)

// The committed reference must match what this generator writes, so currency
// is enforced by `go test ./...` as well as by `just docs-check`.
func TestCommittedPagesMatchTheGenerator(t *testing.T) {
	if err := run(filepath.Join("..", "..", "site", "src", "content", "docs", "reference"), true, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func generated(t *testing.T) map[string]string {
	t.Helper()
	pages, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	return pages
}

func TestCLIReferenceCoversEveryVisibleCommandAndFlag(t *testing.T) {
	page := generated(t)["cli.mdx"]
	app, err := cliModel()
	if err != nil {
		t.Fatal(err)
	}
	var walk func(*kong.Node)
	walk = func(node *kong.Node) {
		for _, child := range node.Children {
			if child.Type != kong.CommandNode {
				continue
			}
			heading := "# " + child.FullPath() + "\n"
			if child.Hidden {
				if strings.Contains(page, heading) {
					t.Errorf("hidden command %q is documented", child.FullPath())
				}
				continue
			}
			if !strings.Contains(page, heading) {
				t.Errorf("command %q has no section", child.FullPath())
			}
			for _, flag := range child.Flags {
				if flag.Hidden {
					continue
				}
				if !strings.Contains(page, "--"+flag.Name) {
					t.Errorf("%s flag --%s is missing", child.FullPath(), flag.Name)
				}
			}
			walk(child)
		}
	}
	walk(app.Node)
	for _, hidden := range []string{"driga hook ", "driga feed", "driga dev", "--chat"} {
		if strings.Contains(page, hidden) {
			t.Errorf("hidden interface %q leaked into the CLI reference", hidden)
		}
	}
}

func TestSettingsReferenceCoversEverySetting(t *testing.T) {
	page := generated(t)["settings.mdx"]
	for _, s := range config.Settings() {
		if !strings.Contains(page, "### "+s.Key+"\n") {
			t.Errorf("setting %s has no section", s.Key)
		}
		if name := s.EnvName(); name != "" && !strings.Contains(page, "`"+name+"`") {
			t.Errorf("setting %s does not name %s", s.Key, name)
		}
	}
	for _, s := range config.RuntimeSettings() {
		if !strings.Contains(page, "`"+s.Name+"`") {
			t.Errorf("runtime name %s is missing", s.Name)
		}
	}
}

func TestMCPReferenceSplitsReadAndChatTools(t *testing.T) {
	page := generated(t)["mcp-tools.mdx"]
	readPart, chatPart, ok := strings.Cut(page, "## Chat tools")
	if !ok {
		t.Fatal("no Chat tools section")
	}
	for _, name := range []string{"get_flow", "list_agents", "inspect_setup", "list_cleanup_candidates"} {
		if !strings.Contains(readPart, "### "+name+"\n") {
			t.Errorf("read tool %s missing", name)
		}
	}
	for _, name := range []string{"answer_prompt", "ack_inbox", "approve", "list_prompts"} {
		if strings.Contains(readPart, "### "+name+"\n") {
			t.Errorf("chat tool %s documented as served by driga mcp", name)
		}
		if !strings.Contains(chatPart, "### "+name+"\n") {
			t.Errorf("chat tool %s missing", name)
		}
	}
}

func TestHooksReferenceReadsTheInstallers(t *testing.T) {
	page := generated(t)["hooks.mdx"]
	for _, want := range []string{"`PermissionRequest` | 660 s", "`sessionStart` | 3 s", "## Codex"} {
		if !strings.Contains(page, want) {
			t.Errorf("hooks page lacks %q", want)
		}
	}
}

func TestTextEscapesMDX(t *testing.T) {
	got := text("Editor command; {cwd} is substituted for <dir> in `{cwd}` *now*")
	want := "Editor command; \\{cwd\\} is substituted for &lt;dir&gt; in `{cwd}` \\*now\\*"
	if got != want {
		t.Fatalf("text() = %q, want %q", got, want)
	}
	if got := cell("a | b\nc"); got != `a \| b c` {
		t.Fatalf("cell() = %q", got)
	}
}

func TestVerifyReportsStaleMissingAndOrphanedPages(t *testing.T) {
	dir := t.TempDir()
	pages := map[string]string{"a.mdx": "A " + generatedMarker, "b.mdx": "B " + generatedMarker}
	if err := write(dir, pages); err != nil {
		t.Fatal(err)
	}
	if err := verify(dir, pages); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.mdx"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old.mdx"), []byte(generatedMarker), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hand.mdx"), []byte("hand-written"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "b.mdx")); err != nil {
		t.Fatal(err)
	}
	err := verify(dir, pages)
	if err == nil {
		t.Fatal("verify accepted stale pages")
	}
	for _, want := range []string{"a.mdx", "b.mdx (missing)", "old.mdx (no longer generated)", "just docs"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "hand.mdx") {
		t.Error("a hand-written page was reported")
	}
	if err := write(dir, pages); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.mdx")); !os.IsNotExist(err) {
		t.Error("write kept an orphaned generated page")
	}
	if _, err := os.Stat(filepath.Join(dir, "hand.mdx")); err != nil {
		t.Error("write removed a hand-written page")
	}
}
