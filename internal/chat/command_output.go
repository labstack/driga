package chat

import (
	"context"
	"errors"
	"github.com/labstack/driga/internal/scrub"
	"github.com/labstack/driga/internal/store"
	"io"
	"strings"
	"sync"
)

// Each stream holds at most one bounded lexical token. Tokens spanning writes
// are scrubbed together; PEM bodies and oversized secret tokens are discarded before
// reaching storage or the agent. No raw output is spooled to disk. The agent
// always gets the scrubbed head and tail of the command it asked to run; only
// snippets capture stores the full scrubbed output.
type outputStream struct {
	parent             *commandOutput
	name, token        string
	overflow, pem, end bool
}
type commandOutput struct {
	mu         sync.Mutex
	st         *store.Store
	action     string
	snippets   bool
	streams    []*outputStream
	seq        int
	head, tail string
	total      int
	err        error
	finished   bool
}

func newCommandOutput(st *store.Store, actionID string, snippets bool) *commandOutput {
	return &commandOutput{st: st, action: actionID, snippets: snippets}
}
func (o *commandOutput) Writer(stream string) io.Writer {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, s := range o.streams {
		if s.name == stream {
			return s
		}
	}
	s := &outputStream{parent: o, name: stream}
	o.streams = append(o.streams, s)
	return s
}
func (o *commandOutput) emit(ctx context.Context, stream, data string) {
	if data == "" || o.err != nil {
		return
	}
	if o.snippets {
		if o.err = o.st.AppendCommandOutput(ctx, o.action, stream, o.seq, []byte(data)); o.err != nil {
			return
		}
		o.seq++
	}
	o.total += len(data)
	if len(o.head) < 2048 {
		n := min(2048-len(o.head), len(data))
		o.head += data[:n]
	}
	o.tail += data
	if len(o.tail) > 2048 {
		o.tail = o.tail[len(o.tail)-2048:]
	}
}
func (s *outputStream) flush() string {
	token := s.token
	s.token = ""
	if s.overflow {
		s.overflow = false
		return "[redacted]"
	}
	if strings.Contains(token, "-----BEGIN") {
		s.pem = true
		return "[redacted]"
	}
	if s.pem {
		if strings.Contains(token, "-----END") {
			s.end = true
		}
		if s.end && strings.Contains(token, "KEY-----") {
			s.pem = false
			s.end = false
		}
		return ""
	}
	return scrub.Scrub(token)
}
func (s *outputStream) Write(p []byte) (int, error) {
	o := s.parent
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err != nil {
		return 0, o.err
	}
	if o.finished {
		return 0, errors.New("command output closed")
	}
	var safe strings.Builder
	for _, b := range p {
		if b == ' ' || b == '\n' || b == '\r' || b == '\t' {
			safe.WriteString(s.flush())
			if !s.pem {
				safe.WriteByte(b)
			}
		} else if !s.overflow {
			if len(s.token) >= 8192 {
				if secretToken(s.token) || s.pem {
					s.token = ""
					s.overflow = true
				} else {
					// Keep enough suffix for a recognizable prefix split at this boundary.
					safe.WriteString(s.token[:len(s.token)-256])
					s.token = s.token[len(s.token)-256:] + string([]byte{b})
				}
			} else {
				s.token += string([]byte{b})
			}
		}
		if safe.Len() >= 8192 {
			o.emit(context.Background(), s.name, safe.String())
			safe.Reset()
			if o.err != nil {
				return 0, o.err
			}
		}
	}
	o.emit(context.Background(), s.name, safe.String())
	if o.err != nil {
		return 0, o.err
	}
	return len(p), nil
}
func (o *commandOutput) Finish(ctx context.Context) (string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.finished {
		for _, s := range o.streams {
			o.emit(ctx, s.name, s.flush())
		}
		o.finished = true
	}
	if o.err != nil {
		return "", o.err
	}
	if o.total <= 2048 {
		return o.head, nil
	}
	if o.total <= 4096 {
		return o.head + o.tail[len(o.tail)-(o.total-2048):], nil
	}
	return o.head + "\n… output truncated …\n" + o.tail, nil
}

func secretToken(token string) bool {
	if scrub.Scrub(token) != token {
		return true
	}
	// JWT's first component and PEM headers can grow before the ordinary
	// complete-secret regex matches. Hold them closed instead of leaking a prefix.
	return strings.Contains(token, "eyJ") || strings.Contains(token, "-----BEGIN")
}
