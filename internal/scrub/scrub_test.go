package scrub

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestScrub(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"anthropic", "sk-ant-api03-" + strings.Repeat("a", 24), "[redacted]"},
		{"openai-project", "sk-proj-" + strings.Repeat("a_B-9", 8), "[redacted]"},
		{"generic-key", "sk-" + strings.Repeat("a", 20), "[redacted]"},
		{"short-key", "sk-" + strings.Repeat("a", 19), "sk-" + strings.Repeat("a", 19)},
		{"github-pat", "github_pat_" + strings.Repeat("a_9", 20), "[redacted]"},
		{"aws-access", "AKIA" + strings.Repeat("A1", 8), "[redacted]"},
		{"aws-session", "ASIA" + strings.Repeat("Z9", 8), "[redacted]"},
		{"short-aws", "AKIA" + strings.Repeat("A", 15), "AKIA" + strings.Repeat("A", 15)},
		{"jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2lnX-8", "[redacted]"},
		{"jwt-payload", "eyJhbGciOiJIUzI1NiJ9.cGF5bG9hZA.c2ln", "[redacted]"},
		{"rsa-pem", "-----BEGIN RSA PRIVATE KEY-----\nYWJj\nZGVm\n-----END RSA PRIVATE KEY-----", "[redacted]"},
		{"pem", "-----BEGIN PRIVATE KEY-----\nYWJj\n-----END PRIVATE KEY-----", "[redacted]"},
		{"encrypted-pem", "-----BEGIN ENCRYPTED PRIVATE KEY-----\r\nYWJj\r\n-----END ENCRYPTED PRIVATE KEY-----", "[redacted]"},
		{"ec-pem-inline", "-----BEGIN EC PRIVATE KEY-----YWJj-----END EC PRIVATE KEY-----", "[redacted]"},
		{"multiple-pem", "-----BEGIN PRIVATE KEY-----a-----END PRIVATE KEY----- keep -----BEGIN RSA PRIVATE KEY-----b-----END RSA PRIVATE KEY-----", "[redacted] keep [redacted]"},
		{"public-pem", "-----BEGIN PUBLIC KEY-----abc-----END PUBLIC KEY-----", "-----BEGIN PUBLIC KEY-----abc-----END PUBLIC KEY-----"},
		{"prose", "sketch a task-123 using this skill; hello world", "sketch a task-123 using this skill; hello world"},
		{"empty", "", ""},
		{"surroundings", "token=sk-" + strings.Repeat("a", 20) + ", done", "token=[redacted], done"},
		{"multiple", "sk-" + strings.Repeat("a", 20) + " and ghp_" + strings.Repeat("b", 36), "[redacted] and [redacted]"},
	}
	for _, prefix := range []string{"ghp_", "gho_", "ghs_", "ghu_"} {
		cases = append(cases, struct{ name, input, want string }{prefix, prefix + strings.Repeat("a", 36), "[redacted]"})
	}
	for _, kind := range "abposr" {
		prefix := "xox" + string(kind) + "-"
		cases = append(cases, struct{ name, input, want string }{prefix, prefix + "123456-abcdef-7890", "[redacted]"})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Scrub(tc.input); got != tc.want {
				t.Errorf("Scrub() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTruncateRuneSafe(t *testing.T) {
	for _, tc := range []struct {
		input string
		n     int
		want  string
	}{
		{"héllo", 3, "hé…"}, {"héllo", 5, "héllo"}, {"héllo", 9, "héllo"},
		{"你好世界", 3, "你好…"}, {"🙂🙃🙂", 2, "🙂…"}, {"abc", 1, "…"},
		{"abc", 0, ""}, {"abc", -1, ""}, {"", 0, ""},
	} {
		got := Truncate(tc.input, tc.n)
		if got != tc.want || !utf8.ValidString(got) || (tc.n >= 0 && utf8.RuneCountInString(got) > tc.n) {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tc.input, tc.n, got, tc.want)
		}
	}
}

func TestSnippetCollapsesWhitespace(t *testing.T) {
	if got := Snippet("  hello\n\tworld\r\n\u2003 again  "); got != "hello world again" {
		t.Fatalf("Snippet() = %q", got)
	}
	if got := Snippet(strings.Repeat("界", 201)); got != strings.Repeat("界", 199)+"…" {
		t.Fatalf("Snippet() did not cap at 200 runes: %d", utf8.RuneCountInString(got))
	}
	if got := Snippet("key\n sk-" + strings.Repeat("a", 25)); got != "key [redacted]" {
		t.Fatalf("Snippet() = %q", got)
	}
	// Redact the whole key before truncating, including collapsed PEM blocks.
	if got := Snippet(strings.Repeat("a", 190) + " sk-" + strings.Repeat("b", 25)); strings.Contains(got, "sk-") {
		t.Fatalf("Snippet() leaked a truncated key: %q", got)
	}
	if got := Snippet("-----BEGIN RSA PRIVATE KEY-----\nabc\n-----END RSA PRIVATE KEY-----"); got != "[redacted]" {
		t.Fatalf("Snippet(PEM) = %q", got)
	}
}
