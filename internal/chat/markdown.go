package chat

import (
	"bytes"
	"html"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// MarkdownDocument owns the source backing its CommonMark + GFM AST. Raw HTML
// remains in the tree for literal terminal display; the safe HTML renderer omits it.
type MarkdownDocument struct {
	Source []byte
	Root   ast.Node
}

// One parser configuration serves both clients. Never enable html.WithUnsafe.
var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute)))

func ParseMarkdown(input string) MarkdownDocument { return parseMarkdown(PlainCitations(input)) }
func parseMarkdown(input string) MarkdownDocument {
	if len(input) > MaxText {
		input = input[:MaxText]
		for !utf8.ValidString(input) && len(input) > 0 {
			input = input[:len(input)-1]
		}
	}
	source := []byte(input)
	return MarkdownDocument{Source: source, Root: markdown.Parser().Parse(text.NewReader(source))}
}

// MarkdownText resolves CommonMark escapes and entities for terminal display.
func MarkdownText(value []byte) string {
	return html.UnescapeString(string(util.UnescapePunctuations(value)))
}

func safeMarkdownURL(value []byte) bool {
	s := MarkdownText(value)
	if strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return false
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return u.Host != ""
	case "mailto":
		return u.Opaque != "" || u.Path != ""
	}
	return false
}

// MarkdownHTML is the only producer of assistant HTML. Images and unsupported
// URL schemes become escaped text, including relative and protocol-relative URLs.
func MarkdownHTML(input string) string {
	d := parseMarkdown(input)
	renderCitations(d)
	var blocked []ast.Node
	_ = ast.Walk(d.Root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var destination []byte
		switch n := n.(type) {
		case *ast.Link:
			destination = n.Destination
		case *ast.Image:
			destination = n.Destination
		case *ast.AutoLink:
			destination = n.URL(d.Source)
			if n.AutoLinkType == ast.AutoLinkEmail && !bytes.HasPrefix(destination, []byte("mailto:")) {
				destination = append([]byte("mailto:"), destination...)
			}
		default:
			return ast.WalkContinue, nil
		}
		// Images never load from assistant output: a remote image is a request
		// a prompt-injected reply could use to exfiltrate data.
		if _, image := n.(*ast.Image); image || !safeMarkdownURL(destination) {
			blocked = append(blocked, n)
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	for _, n := range blocked {
		label := MarkdownText(n.Text(d.Source))
		var destination []byte
		switch n := n.(type) {
		case *ast.Link:
			destination = n.Destination
		case *ast.Image:
			destination = n.Destination
			label = "[image: " + label + "]"
		case *ast.AutoLink:
			destination = n.URL(d.Source)
		}
		if label != string(destination) {
			label += " (" + MarkdownText(destination) + ")"
		}
		// The label was unescaped for display; escape it again for HTML.
		replacement := ast.NewString([]byte(html.EscapeString(label)))
		replacement.SetRaw(true)
		parent := n.Parent()
		parent.ReplaceChild(parent, n, replacement)
	}
	var out bytes.Buffer
	// bytes.Buffer writes cannot fail.
	_ = markdown.Renderer().Render(&out, d.Source, d.Root)
	return out.String()
}

// Only digit IDs from the citation grammar enter generated markup. Raw assistant
// HTML remains disabled. Code examples and link labels stay literal.
func renderCitations(d MarkdownDocument) {
	var nodes []*ast.Text
	_ = ast.Walk(d.Root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if _, ok := n.(*ast.CodeSpan); ok {
			return ast.WalkSkipChildren, nil
		}
		if _, ok := n.(*ast.Link); ok {
			return ast.WalkSkipChildren, nil
		}
		if _, ok := n.(*ast.Image); ok {
			return ast.WalkSkipChildren, nil
		}
		if n, ok := n.(*ast.Text); ok {
			nodes = append(nodes, n)
		}
		return ast.WalkContinue, nil
	})
	for _, n := range nodes {
		if n.Parent() == nil {
			continue
		}
		for {
			next, ok := n.NextSibling().(*ast.Text)
			if !ok || n.Segment.Stop != next.Segment.Start || n.SoftLineBreak() || n.HardLineBreak() {
				break
			}
			n.Segment.Stop = next.Segment.Stop
			n.SetSoftLineBreak(next.SoftLineBreak())
			n.SetHardLineBreak(next.HardLineBreak())
			n.Parent().RemoveChild(n.Parent(), next)
		}
		if !citation.Match(n.Segment.Value(d.Source)) {
			continue
		}
		value := string(n.Segment.Value(d.Source))
		var out strings.Builder
		end := 0
		for _, match := range citation.FindAllStringSubmatchIndex(value, -1) {
			out.WriteString(html.EscapeString(MarkdownText([]byte(value[end:match[0]]))))
			id := value[match[2]:match[3]]
			out.WriteString(`<button type="button" class="chat-chip" data-chat-session="` + id + `">session #` + id + `</button>`)
			end = match[1]
		}
		out.WriteString(html.EscapeString(MarkdownText([]byte(value[end:]))))
		if n.HardLineBreak() {
			out.WriteString("<br>\n")
		} else if n.SoftLineBreak() {
			out.WriteByte('\n')
		}
		replacement := ast.NewString([]byte(out.String()))
		replacement.SetCode(true)
		n.Parent().ReplaceChild(n.Parent(), n, replacement)
	}
}
