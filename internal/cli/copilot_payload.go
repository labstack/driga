package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// Large results and error stacks are consumed without allocating their contents. Retained fields
// still share the ordinary 1 MiB bound; total stdin is limited to 64 MiB and the
// same read deadline as other adapters. Deeply nested input is rejected too.
const copilotInputLimit = 64 << 20

func hookReadCopilotInput(input io.Reader) ([]byte, error) {
	return hookReadBounded(input, func(r io.Reader) ([]byte, error) {
		p := copilotJSONFilter{r: bufio.NewReader(io.LimitReader(r, copilotInputLimit+1))}
		if err := p.value(true, 0); err != nil {
			return nil, err
		}
		if err := p.space(); err != nil {
			return nil, err
		}
		if _, err := p.r.Peek(1); err != io.EOF {
			return nil, errors.New("trailing Copilot payload")
		}
		return p.out.Bytes(), nil
	})
}

type copilotJSONFilter struct {
	r    *bufio.Reader
	out  bytes.Buffer
	read int
}

func (p *copilotJSONFilter) take(keep bool) (byte, error) {
	b, err := p.r.ReadByte()
	if err != nil {
		return 0, err
	}
	p.read++
	if p.read > copilotInputLimit {
		return 0, errors.New("Copilot payload exceeds 64 MiB")
	}
	if keep {
		if p.out.Len() >= hookPayloadLimit {
			return 0, errors.New("Copilot metadata exceeds 1 MiB")
		}
		p.out.WriteByte(b)
	}
	return b, nil
}

func (p *copilotJSONFilter) space() error {
	for {
		b, err := p.r.Peek(1)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if !bytes.ContainsRune([]byte(" \t\r\n"), rune(b[0])) {
			return nil
		}
		if _, err := p.take(false); err != nil {
			return err
		}
	}
}

func (p *copilotJSONFilter) expect(want byte, keep bool) error {
	if err := p.space(); err != nil {
		return err
	}
	b, err := p.take(keep)
	if err != nil {
		return err
	}
	if b != want {
		return errors.New("invalid Copilot JSON delimiter")
	}
	return nil
}

func (p *copilotJSONFilter) quoted(keep bool) error {
	if err := p.expect('"', keep); err != nil {
		return err
	}
	for {
		b, err := p.take(keep)
		if err != nil {
			return err
		}
		switch {
		case b == '"':
			return nil
		case b < 0x20:
			return errors.New("invalid Copilot JSON string")
		case b == '\\':
			esc, err := p.take(keep)
			if err != nil {
				return err
			}
			if esc == 'u' {
				for range 4 {
					h, err := p.take(keep)
					if err != nil {
						return err
					}
					if !(h >= '0' && h <= '9' || h >= 'a' && h <= 'f' || h >= 'A' && h <= 'F') {
						return errors.New("invalid Copilot JSON escape")
					}
				}
			} else if !bytes.ContainsRune([]byte(`"\/bfnrt`), rune(esc)) {
				return errors.New("invalid Copilot JSON escape")
			}
		}
	}
}

func (p *copilotJSONFilter) value(keep bool, depth int) error {
	if depth > 128 {
		return errors.New("Copilot JSON nesting exceeds limit")
	}
	if err := p.space(); err != nil {
		return err
	}
	b, err := p.r.Peek(1)
	if err != nil {
		return err
	}
	switch b[0] {
	case '"':
		return p.quoted(keep)
	case '{', '[':
		object := b[0] == '{'
		end := byte(']')
		if object {
			end = '}'
		}
		if _, err := p.take(keep); err != nil {
			return err
		}
		if err := p.space(); err != nil {
			return err
		}
		b, err := p.r.Peek(1)
		if err != nil {
			return err
		}
		if b[0] == end {
			_, err := p.take(keep)
			return err
		}
		for {
			skip := false
			if object {
				start := p.out.Len()
				if err := p.quoted(keep); err != nil {
					return err
				}
				if keep {
					var key string
					if err := json.Unmarshal(p.out.Bytes()[start:], &key); err != nil {
						return err
					}
					skip = key == "stack" || depth == 0 && (key == "toolResult" || key == "tool_result")
				}
				if err := p.expect(':', keep); err != nil {
					return err
				}
			}
			if err := p.value(keep && !skip, depth+1); err != nil {
				return err
			}
			if skip {
				p.out.WriteString("null")
			}
			if err := p.space(); err != nil {
				return err
			}
			delim, err := p.take(keep)
			if err != nil {
				return err
			}
			if delim == end {
				return nil
			}
			if delim != ',' {
				return errors.New("invalid Copilot JSON separator")
			}
		}
	default:
		return p.primitive(keep)
	}
}

// Validate literals and numbers while streaming, including results that are a
// very large JSON number. No discarded token needs a scratch allocation.
func (p *copilotJSONFilter) primitive(keep bool) error {
	first, err := p.r.Peek(1)
	if err != nil {
		return err
	}
	literal := ""
	switch first[0] {
	case 't':
		literal = "true"
	case 'f':
		literal = "false"
	case 'n':
		literal = "null"
	}
	if literal != "" {
		for i := range len(literal) {
			b, err := p.take(keep)
			if err != nil {
				return err
			}
			if b != literal[i] {
				return errors.New("invalid Copilot JSON literal")
			}
		}
		return nil
	}
	// States: start, sign, zero, integer, decimal point, fraction, e, exponent
	// sign, exponent digits. Only zero/integer/fraction/exponent digits may end.
	state := 0
	for {
		next, err := p.r.Peek(1)
		if err != nil && err != io.EOF {
			return err
		}
		if err == io.EOF || bytes.ContainsRune([]byte(" \t\r\n,]}"), rune(next[0])) {
			break
		}
		b, err := p.take(keep)
		if err != nil {
			return err
		}
		digit := b >= '0' && b <= '9'
		switch state {
		case 0, 1:
			if state == 0 && b == '-' {
				state = 1
			} else if b == '0' {
				state = 2
			} else if b >= '1' && b <= '9' {
				state = 3
			} else {
				return errors.New("invalid Copilot JSON number")
			}
		case 2, 3:
			if state == 3 && digit {
				continue
			}
			if b == '.' {
				state = 4
			} else if b == 'e' || b == 'E' {
				state = 6
			} else {
				return errors.New("invalid Copilot JSON number")
			}
		case 4, 5:
			if digit {
				state = 5
			} else if state == 5 && (b == 'e' || b == 'E') {
				state = 6
			} else {
				return errors.New("invalid Copilot JSON number")
			}
		case 6, 7, 8:
			if digit {
				state = 8
			} else if state == 6 && (b == '+' || b == '-') {
				state = 7
			} else {
				return errors.New("invalid Copilot JSON number")
			}
		}
	}
	if state != 2 && state != 3 && state != 5 && state != 8 {
		return errors.New("invalid Copilot JSON number")
	}
	return nil
}
