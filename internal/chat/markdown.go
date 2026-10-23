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

func ParseMarkdown(input string) MarkdownDocument {
	if len(input) > MaxText {
		input = input[:MaxText]
		for !utf8.ValidString(input) && len(input) > 0 {
			input = input[:len(input)-1]
		}
	}
	source := []byte(PlainCitations(input))
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

// MarkdownHTML is the only producer of assistant HTML. Unsupported URL schemes
// become escaped text, including relative URLs and protocol-relative references.
func MarkdownHTML(input string) string {
	d := ParseMarkdown(input)
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
		if !safeMarkdownURL(destination) {
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
		replacement := ast.NewString([]byte(label))
		replacement.SetRaw(true)
		parent := n.Parent()
		parent.ReplaceChild(parent, n, replacement)
	}
	var out bytes.Buffer
	// bytes.Buffer writes cannot fail.
	_ = markdown.Renderer().Render(&out, d.Source, d.Root)
	return out.String()
}
