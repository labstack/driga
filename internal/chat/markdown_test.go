package chat

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
)

// The same cases run through the real browser renderer in TestChatDOM. The
// neutral tree below compares grammar without depending on either renderer.
type markdownTestNode struct {
	Tag      string             `json:"tag"`
	Text     string             `json:"text,omitempty"`
	Children []markdownTestNode `json:"children,omitempty"`
}

func markdownTestText(text string) []markdownTestNode {
	if text == "" {
		return nil
	}
	return []markdownTestNode{{Tag: "#text", Text: text}}
}

func markdownTestSpans(spans []MarkdownSpan) []markdownTestNode {
	var nodes []markdownTestNode
	for _, span := range spans {
		if span.Kind == "text" {
			if span.Text == "" {
				continue
			}
			if len(nodes) > 0 && nodes[len(nodes)-1].Tag == "#text" {
				nodes[len(nodes)-1].Text += span.Text
			} else {
				nodes = append(nodes, markdownTestText(span.Text)...)
			}
		} else {
			children := markdownTestSpans(span.Children)
			if span.Kind == "code" {
				children = markdownTestText(span.Text)
			}
			nodes = append(nodes, markdownTestNode{Tag: span.Kind, Children: children})
		}
	}
	return nodes
}

func TestMarkdownGrammar(t *testing.T) {
	data, err := os.ReadFile("testdata/markdown.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Input string
		Want        []markdownTestNode
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			var got []markdownTestNode
			for _, block := range ParseMarkdown(tc.Input) {
				node := markdownTestNode{Tag: block.Kind, Children: markdownTestSpans(block.Spans)}
				switch block.Kind {
				case "paragraph":
					node.Tag = "p"
				case "heading":
					node.Tag = fmt.Sprintf("h%d", block.Level)
				case "code":
					node.Tag = "pre"
					node.Children = []markdownTestNode{{Tag: "code", Children: markdownTestText(block.Text)}}
				case "ul", "ol":
					for _, item := range block.Items {
						node.Children = append(node.Children, markdownTestNode{Tag: "li", Children: markdownTestSpans(item)})
					}
				}
				got = append(got, node)
			}
			if !reflect.DeepEqual(got, tc.Want) {
				actual, _ := json.Marshal(got)
				want, _ := json.Marshal(tc.Want)
				t.Fatalf("got  %s\nwant %s", actual, want)
			}
		})
	}
}

func TestMarkdownInlineDepthLimit(t *testing.T) {
	if got := parseMarkdownInline("**literal**", 17); !reflect.DeepEqual(got, []MarkdownSpan{{Kind: "text", Text: "**literal**"}}) {
		t.Fatal(got)
	}
	if got := parseMarkdownInline("**_last_**", 16); len(got) != 1 || got[0].Kind != "strong" || got[0].Children[0].Text != "_last_" {
		t.Fatal(got)
	}
}
