package command

import (
	"fmt"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

func Parse(in Request) (Parsed, error) {
	if (len(in.Argv) > 0) == (in.Script != "") {
		return Parsed{}, fmt.Errorf("exactly one of argv or script is required")
	}
	if in.TimeoutSeconds < 0 || in.TimeoutSeconds > 600 {
		return Parsed{}, fmt.Errorf("timeout_seconds must be between 1 and 600")
	}
	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = 120
	}
	if strings.ContainsRune(in.Cwd, 0) {
		return Parsed{}, fmt.Errorf("NUL in cwd")
	}
	if in.Cwd != "" {
		var err error
		in.Cwd, err = filepath.Abs(in.Cwd)
		if err != nil {
			return Parsed{}, err
		}
	}
	in.Argv = append([]string(nil), in.Argv...)
	p := Parsed{Request: in}
	if len(in.Argv) > 0 {
		if in.Argv[0] == "" {
			return Parsed{}, fmt.Errorf("empty executable")
		}
		for _, s := range in.Argv {
			if strings.ContainsRune(s, 0) {
				return Parsed{}, fmt.Errorf("NUL in argv")
			}
		}
		p.Commands = [][]string{append([]string(nil), in.Argv...)}
		return p, nil
	}
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(in.Script), "")
	if err != nil {
		return Parsed{}, err
	}
	if len(f.Stmts) != 1 {
		p.Complex = true
	}
	syntax.Walk(f, func(n syntax.Node) bool {
		switch n := n.(type) {
		case nil:
		case *syntax.File, *syntax.Word, *syntax.Lit, *syntax.SglQuoted, *syntax.DblQuoted:
		case *syntax.Stmt:
			if n.Background || n.Negated || n.Coprocess || len(n.Redirs) > 0 {
				p.Complex = true
			}
		case *syntax.BinaryCmd:
			if n.Op != syntax.Pipe {
				p.Complex = true
			}
			p.Pipeline = true
		case *syntax.CallExpr:
			if len(n.Assigns) > 0 {
				p.Complex = true
			}
			var args []string
			for _, w := range n.Args {
				s, ok := literal(w.Parts, false)
				if !ok {
					p.Complex = true
				}
				args = append(args, s)
			}
			if len(args) > 0 {
				p.Commands = append(p.Commands, args)
			}
		default:
			p.Complex = true
		}
		return true
	})
	if len(p.Commands) == 0 {
		p.Complex = true
	}
	return p, nil
}

func literal(parts []syntax.WordPart, quoted bool) (string, bool) {
	var b strings.Builder
	for _, part := range parts {
		switch n := part.(type) {
		case *syntax.Lit:
			for i := 0; i < len(n.Value); i++ {
				c := n.Value[i]
				if c == '\\' && i+1 < len(n.Value) {
					i++
					if quoted && !strings.ContainsRune("\\\"$`\n", rune(n.Value[i])) {
						b.WriteByte('\\')
					}
					if n.Value[i] != '\n' {
						b.WriteByte(n.Value[i])
					}
					continue
				}
				if !quoted && strings.ContainsRune("*?[~{}", rune(c)) {
					return "", false
				}
				b.WriteByte(c)
			}
		case *syntax.SglQuoted:
			if n.Dollar {
				return "", false
			}
			b.WriteString(n.Value)
		case *syntax.DblQuoted:
			s, ok := literal(n.Parts, true)
			if !ok {
				return "", false
			}
			b.WriteString(s)
		default:
			return "", false
		}
	}
	return b.String(), true
}
