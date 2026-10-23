package chat

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MarkdownBlock is one block in the deliberately small chat Markdown grammar.
// Lists contain consecutive items of the same kind and are numbered from one.
// Code is verbatim; paragraphs retain single newlines. HTML is always plain text.
type MarkdownBlock struct {
	Kind  string // paragraph, heading, code, ul, ol
	Level int    // ATX heading level, 1–6
	Text  string // code only, without the fences or language label
	Spans []MarkdownSpan
	Items [][]MarkdownSpan
}

// MarkdownSpan is text, code, strong, or em. Only strong and em have children.
// Links are plain text followed by " (url)"; they never become terminal links.
type MarkdownSpan struct {
	Kind     string
	Text     string
	Children []MarkdownSpan
}

var (
	markdownFence   = regexp.MustCompile("^\\s*(`{3,}|~{3,})")
	markdownList    = regexp.MustCompile(`^\s*(?:([-+*])|([0-9]+)[.)])\s+(.*)$`)
	markdownHeading = regexp.MustCompile(`^\s*(#{1,6})(?:[ \t]+(.*))?$`)
	markdownCloser  = regexp.MustCompile(`[ \t]+#+[ \t]*$`)
	markdownInline  = []*regexp.Regexp{
		regexp.MustCompile(`^\*\*([^\n]+?)\*\*`),
		regexp.MustCompile(`^__([^\n]+?)__`),
		regexp.MustCompile(`^\*([^*\n]+?)\*`),
		regexp.MustCompile(`^_([^_\n]+?)_`),
		regexp.MustCompile(`^\[([^\]\n]+)\]\(([^)\n]*)\)`),
	}
)

// ParseMarkdown defines the safe chat subset used by terminal clients. Keep the
// browser's text-node-only renderer and its DOM cases aligned with this grammar.
// There are no escapes, raw HTML, nested lists, images, or other Markdown forms.
// A fence closes with the same character and at least the opening run length;
// an unterminated fence consumes the rest of the message. Inline nesting stops
// after 17 emphasis levels, matching the browser's depth > 16 guard.
func ParseMarkdown(text string) []MarkdownBlock {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(text, "\n")
	var blocks []MarkdownBlock
	for i := 0; i < len(lines); {
		if strings.TrimSpace(lines[i]) == "" {
			i++
			continue
		}
		if start := markdownFence.FindStringSubmatch(lines[i]); start != nil {
			i++
			var body []string
			for i < len(lines) {
				end := markdownFence.FindStringSubmatch(lines[i])
				if end != nil && end[1][0] == start[1][0] && len(end[1]) >= len(start[1]) {
					i++
					break
				}
				body = append(body, lines[i])
				i++
			}
			blocks = append(blocks, MarkdownBlock{Kind: "code", Text: strings.Join(body, "\n")})
			continue
		}
		if heading := markdownHeading.FindStringSubmatch(lines[i]); heading != nil {
			body := strings.TrimSpace(markdownCloser.ReplaceAllString(heading[2], ""))
			blocks = append(blocks, MarkdownBlock{Kind: "heading", Level: len(heading[1]), Spans: parseMarkdownInline(body, 0)})
			i++
			continue
		}
		if first := markdownList.FindStringSubmatch(lines[i]); first != nil {
			kind := "ul"
			if first[2] != "" {
				kind = "ol"
			}
			block := MarkdownBlock{Kind: kind}
			for i < len(lines) {
				item := markdownList.FindStringSubmatch(lines[i])
				if item == nil || (item[2] != "") != (kind == "ol") {
					break
				}
				block.Items = append(block.Items, parseMarkdownInline(item[3], 0))
				i++
			}
			blocks = append(blocks, block)
			continue
		}
		body := []string{lines[i]}
		i++
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" && !markdownFence.MatchString(lines[i]) && !markdownList.MatchString(lines[i]) && !markdownHeading.MatchString(lines[i]) {
			body = append(body, lines[i])
			i++
		}
		blocks = append(blocks, MarkdownBlock{Kind: "paragraph", Spans: parseMarkdownInline(strings.Join(body, "\n"), 0)})
	}
	return blocks
}

func parseMarkdownInline(text string, depth int) []MarkdownSpan {
	if depth > 16 {
		return []MarkdownSpan{{Kind: "text", Text: text}}
	}
	var spans []MarkdownSpan
	end := 0
scan:
	for i := 0; i < len(text); {
		var span MarkdownSpan
		n := 0
		if text[i] == '`' {
			n, span.Text = markdownCodeSpan(text[i:])
			span.Kind = "code"
		} else if strings.ContainsRune("*_[", rune(text[i])) {
			for j, pattern := range markdownInline {
				match := pattern.FindStringSubmatch(text[i:])
				if match == nil {
					continue
				}
				n = len(match[0])
				if j == 1 || j == 3 {
					before, _ := utf8.DecodeLastRuneInString(text[:i])
					after, _ := utf8.DecodeRuneInString(text[i+n:])
					if unicode.IsLetter(before) || unicode.IsNumber(before) || unicode.IsLetter(after) || unicode.IsNumber(after) {
						// A skipped match is consumed by the browser's regex,
						// but remains part of the pending literal text.
						i += n
						continue scan
					}
				}
				if j == 4 {
					span = MarkdownSpan{Kind: "text", Text: match[1] + " (" + match[2] + ")"}
				} else {
					span.Kind = "em"
					if j < 2 {
						span.Kind = "strong"
					}
					span.Children = parseMarkdownInline(match[1], depth+1)
				}
				break
			}
		}
		if n == 0 {
			i++
			continue
		}
		if i > end {
			spans = append(spans, MarkdownSpan{Kind: "text", Text: text[end:i]})
		}
		spans = append(spans, span)
		i += n
		end = i
	}
	if end < len(text) {
		spans = append(spans, MarkdownSpan{Kind: "text", Text: text[end:]})
	}
	return spans
}

// markdownCodeSpan matches the browser's (`+)([\s\S]*?)\1 without regex
// backreferences. Choose the longest opening run that has a closing run, then
// the first such close. Inspect runs once rather than retrying every length.
func markdownCodeSpan(text string) (int, string) {
	run := 0
	for run < len(text) && text[run] == '`' {
		run++
	}
	length := run / 2 // The opening run itself can backtrack to an empty span.
	for i := run; i < len(text); {
		at := strings.IndexByte(text[i:], '`')
		if at < 0 {
			break
		}
		i += at
		end := i
		for end < len(text) && text[end] == '`' {
			end++
		}
		length = max(length, min(run, end-i))
		if length == run {
			break
		}
		i = end
	}
	if length == 0 {
		return 0, ""
	}
	close := length + strings.Index(text[length:], strings.Repeat("`", length))
	return close + length, text[length:close]
}
