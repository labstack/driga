package chat

import (
	"regexp"
	"strings"
)

var citation = regexp.MustCompile(`\[\[session:([1-9][0-9]*)\]\]`)

// PlainCitations renders session citations for text-only clients.
func PlainCitations(s string) string { return citation.ReplaceAllString(s, "#$1") }

// StreamSafePrefix drops a trailing "[" or unfinished "[[…" so streamed text
// is only printed once any citation in it is complete.
func StreamSafePrefix(s string) string {
	i := strings.LastIndex(s, "[")
	if i < 0 || strings.Contains(s[i:], "]") {
		return s
	}
	if i > 0 && s[i-1] == '[' {
		i--
	}
	return s[:i]
}
