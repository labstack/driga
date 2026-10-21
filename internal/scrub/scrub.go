// Package scrub redacts recognizable secrets and bounds captured text.
package scrub

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*KEY-----`),
	regexp.MustCompile(`\bsk-(?:(?:ant|proj)-[A-Za-z0-9_-]+|[A-Za-z0-9_-]{20,})`),
	regexp.MustCompile(`\bgh[posu]_[A-Za-z0-9_]+`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]+`),
	regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9_-]+`),
	regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]*\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`),
}

// Scrub replaces recognizable API keys, tokens and private keys with [redacted].
func Scrub(s string) string {
	for _, pattern := range secretPatterns {
		s = pattern.ReplaceAllString(s, "[redacted]")
	}
	return s
}

// Snippet collapses whitespace, scrubs secrets, and limits text to 200 runes.
func Snippet(s string) string {
	return Truncate(Scrub(strings.Join(strings.Fields(s), " ")), 200)
}

// Truncate limits s to n runes, including a final ellipsis when it is cut.
// A nonpositive limit returns an empty string.
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	remaining := n - 1
	for i := range s {
		if remaining == 0 {
			return s[:i] + "…"
		}
		remaining--
	}
	return s
}
