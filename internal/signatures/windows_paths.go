package signatures

import (
	"strings"
)

// Only Windows-shaped inputs are adapted. Match still starts at argv[0], so
// mentioning an agent in another program's arguments cannot classify it.
func windowsSignatureInput(cmd, exe string) (string, string) {
	if !windowsPathShape(exe) && !windowsPathShape(cmd) {
		return cmd, exe
	}
	exe = windowsSignaturePath(exe, true)
	if cmd == "" {
		return cmd, exe
	}
	args := windowsWords(cmd)
	if len(args) == 0 {
		return cmd, exe
	}
	args[0] = windowsSignaturePath(args[0], true)
	if len(args) > 1 {
		args[1] = windowsSignaturePath(args[1], false)
	}
	for i := range args {
		args[i] = strings.ReplaceAll(strings.ReplaceAll(args[i], " ", "%20"), "\t", "%09")
	}
	return strings.Join(args, " "), exe
}

func windowsPathShape(p string) bool {
	p = strings.TrimPrefix(p, `"`)
	return len(p) > 2 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') || strings.HasPrefix(p, `\\`)
}
func windowsSignaturePath(p string, executable bool) string {
	p = strings.ReplaceAll(strings.ToLower(p), `\`, "/")
	p = strings.TrimPrefix(p, "//?/")
	if executable {
		p = strings.TrimSuffix(p, ".exe")
	}
	// Regex path tokens cannot contain spaces. This is a matching-only view;
	// the original command line and executable remain in the snapshot.
	return strings.ReplaceAll(p, " ", "%20")
}

// windowsWords follows the Windows argv quoting rule: backslashes are literal
// except directly before a quote, where pairs escape themselves and an odd
// final backslash escapes the quote. No shell expansion or execution occurs.
func windowsWords(s string) []string {
	var out []string
	for i := 0; i < len(s); {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i == len(s) {
			break
		}
		var b strings.Builder
		quoted := false
		for i < len(s) {
			if !quoted && (s[i] == ' ' || s[i] == '\t') {
				break
			}
			n := 0
			for i < len(s) && s[i] == '\\' {
				n++
				i++
			}
			if i < len(s) && s[i] == '"' {
				b.WriteString(strings.Repeat(`\`, n/2))
				if n%2 == 1 {
					b.WriteByte('"')
				} else if quoted && i+1 < len(s) && s[i+1] == '"' {
					b.WriteByte('"')
					i++
				} else {
					quoted = !quoted
				}
				i++
				continue
			}
			b.WriteString(strings.Repeat(`\`, n))
			if i == len(s) || !quoted && (s[i] == ' ' || s[i] == '\t') {
				break
			}
			b.WriteByte(s[i])
			i++
		}
		out = append(out, b.String())
	}
	return out
}
