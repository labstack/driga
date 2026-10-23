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
	if got := MarkdownHTML("**[[session:42]]**"); got != "<p><strong>#42</strong></p>\n" {
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
	for _, input := range []string{"[safe](https://example.com)", "[safe](http://example.com)", "[safe](mailto:a@example.com)", "https://example.com", "<a@example.com>", "![alt](https://example.com/x.png)"} {
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
