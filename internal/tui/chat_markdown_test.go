package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

func TestChatMarkdownWidthsAndNoColor(t *testing.T) {
	const input = "# **Heading** _em_\n\n**Long bold 界界 e\u0301 👨‍👩‍👧‍👦 text** then `code` and [[session:42]].\n\n- **wrapping list 界界**\n\n1. wrapping ordered list\n\n```\n\twide 界界 and very long code\n\nraw **markdown**\n```\n\n<script>literal</script>\x1b[31m\x1b]8;;https://example.com\a"
	for _, width := range []int{1, 2, 3, 8, 20, 24, 60, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			var plain string
			for _, noColor := range []bool{true, false} {
				got := chatMarkdown(chat.PlainCitations(input), width, noColor)
				if !utf8.ValidString(got) {
					t.Fatal("split UTF-8")
				}
				if noColor {
					plain = got
					if strings.ContainsAny(got, "\x1b\a") {
						t.Fatal("NoColor or untrusted text emitted terminal controls")
					}
				} else if ansi.Strip(got) != plain {
					t.Fatalf("styling changed layout:\n%q\n%q", ansi.Strip(got), plain)
				}
				for _, line := range strings.Split(got, "\n") {
					if ansi.StringWidth(line) > width {
						t.Fatalf("width %d overflow: %q", width, line)
					}
					if strings.Contains(ansi.Strip(line), "\x1b") {
						t.Fatalf("broken ANSI: %q", line)
					}
				}
			}
		})
	}
}

func TestChatMarkdownWrappedStyles(t *testing.T) {
	got := strings.Split(chatMarkdown("**abcdefgh**", 4, false), "\n")
	want := []string{
		view.TokenStyle("ink", false).Bold(true).Render("abcd"),
		view.TokenStyle("ink", false).Bold(true).Render("efgh"),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("wrapped lines must have independent styles: %q", got)
	}
}

func TestChatMarkdownAssistantOnly(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.width, m.height, m.tab = 100, 60, chatTab
	m.chatMessages = []store.ChatMessage{
		{Role: "user", Text: "# **user** `raw`\n- user item"},
		{Role: "assistant", Text: "# **assistant** `code`\n- item [[session:42]]"},
	}
	got := m.chatView()
	for _, want := range []string{"# **user** `raw`\n- user item", "assistant code\n\n• item #42"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestChatMarkdownElements(t *testing.T) {
	input, err := os.ReadFile("testdata/markdown.md")
	if err != nil {
		t.Fatal(err)
	}
	defer view.SetTheme("auto")
	for _, width := range []int{120, 60, 24} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			plain := chatMarkdown(string(input), width, true)
			path := filepath.Join("testdata", fmt.Sprintf("markdown-%d.golden", width))
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(path, []byte(plain+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if plain+"\n" != string(want) {
				t.Fatalf("Markdown layout differs from %s:\n%s", path, plain)
			}
			var mocha string
			for _, theme := range []string{"mocha", "latte"} {
				_ = view.SetTheme(theme)
				colored := chatMarkdown(string(input), width, false)
				if ansi.Strip(colored) != plain {
					t.Fatalf("%s changed layout", theme)
				}
				for _, line := range strings.Split(colored, "\n") {
					if ansi.StringWidth(line) > width {
						t.Fatalf("overflow: %q", line)
					}
				}
				if theme == "mocha" {
					mocha = colored
				} else if colored == mocha {
					t.Fatal("theme colors did not switch")
				}
			}
		})
	}
}

func TestChatMarkdownPartial(t *testing.T) {
	for _, input := range []string{"```go\nunterminated fence", "| A | B |\n| --", "| A | B |\n| --- | --- |\n| x", "**partial"} {
		for _, width := range []int{120, 60, 24} {
			for _, noColor := range []bool{true, false} {
				_ = chatMarkdown(input, width, noColor)
			}
		}
	}
}

func TestChatMarkdownStructure(t *testing.T) {
	input := "3. first\n   - nested\n\n> outer\n> > inner\n\n- [ ] pending\n- [x] done\n\n[link](https://example.com) <https://example.com> ![alt](https://example.com/a.png)\n\n<script>literal</script>\n\nline  \nbreak\nsoft\nline"
	got := chatMarkdown(input, 120, true)
	for _, want := range []string{"3. first\n   • nested", "│ outer\n│ \n│ │ inner", "☐ pending", "☑ done", "link (https://example.com)", "[image: alt] (https://example.com/a.png)", "<script>literal</script>", "line\nbreak soft line"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "https://example.com (https://example.com)") {
		t.Fatal("duplicate autolink URL")
	}
}
