package tui

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

func TestChatMarkdownLayout(t *testing.T) {
	const input = "# Title\n\n**Bold** *em* `code` [link](url)\n\n- abcdefghij\n- next\n\n9) abcdefghij\n\n~~~go\n  abcdefghijklmnop\n~~~"
	const want = "Title\n\nBold em co\nde link (u\nrl)\n\n• abcdefgh\n  ij\n• next\n\n1. abcdefg\n   hij\n\n│   abcde…"
	if got := chatMarkdown(input, 10, true); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	colored := chatMarkdown(input, 80, false)
	for _, styled := range []string{
		view.TokenStyle("accent", false).Bold(true).Render("Title"),
		view.TokenStyle("ink", false).Bold(true).Render("Bold"),
		view.TokenStyle("ink", false).Italic(true).Render("em"),
		view.TokenStyle("ink", false).Background(view.ThemeColor("sunk")).Render("code"),
		view.TokenStyle("ink", false).Background(view.ThemeColor("sunk")).Render("│   abcdefghijklmnop"),
	} {
		if !strings.Contains(colored, styled) {
			t.Errorf("missing style %q in %q", styled, colored)
		}
	}
}

func TestChatMarkdownWidthsAndNoColor(t *testing.T) {
	const input = "# **Heading** _em_\n\n**Long bold 界界 e\u0301 👨‍👩‍👧‍👦 text** then `code` and [[session:42]].\n\n- **wrapping list 界界**\n\n1. wrapping ordered list\n\n```\n\twide 界界 and very long code\n\nraw **markdown**\n```\n\n<script>literal</script>\x1b[31m\x1b]8;;https://example.com\a"
	for _, width := range []int{1, 2, 3, 8, 20, 80} {
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
