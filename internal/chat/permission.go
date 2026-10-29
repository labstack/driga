package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

const promptTranscriptLimit = 256 << 10
const promptPayloadLimit = 1 << 20

// PermissionRequest is the parsed agent permission hook input.
type PermissionRequest struct {
	Session    string
	Cwd        string
	Transcript string
	Tool       string
	Input      json.RawMessage
}

// WaitPermission waits for a person through a Chat card or a matching terminal transcript answer.
// The caller owns payload parsing, secure file opening and fail-open transport.
func WaitPermission(parent context.Context, c *Remote, in PermissionRequest, started time.Time, open func(string) (*os.File, error)) (PromptDecision, error) {
	ctx, cancel := context.WithTimeout(parent, ApprovalTTL)
	defer cancel()
	watch, err := newPromptTranscript(in, started, open)
	if err != nil {
		return PromptDecision{}, err
	}
	defer watch.Close()
	answered, err := watch.answered()
	if err != nil {
		return PromptDecision{}, err
	}
	if answered {
		return PromptDecision{}, nil
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	stopWatch := sync.OnceFunc(func() { close(done); <-stopped })
	defer stopWatch()
	go func() {
		defer close(stopped)
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				answered, err := watch.answered()
				if err != nil || answered {
					cancel()
					return
				}
			}
		}
	}()
	out, err := c.WaitPrompt(ctx, Prompt{ID: uuid.NewString(), Session: in.Session, Cwd: in.Cwd, Tool: in.Tool, Input: in.Input})
	stopWatch()
	if err != nil {
		return PromptDecision{}, err
	}
	if err := ctx.Err(); err != nil {
		return PromptDecision{}, err
	}
	if out.Behavior != "allow" && out.Behavior != "deny" {
		return PromptDecision{}, nil
	}
	// The watcher is stopped; retain its matched tool id for the final check,
	// even if the tool_use has since fallen outside the bounded tail.
	answered, err = watch.answered()
	if err != nil {
		return PromptDecision{}, err
	}
	if err := ctx.Err(); err != nil {
		return PromptDecision{}, err
	}
	if answered {
		return PromptDecision{}, nil
	}

	return out, nil
}

// promptTranscript incrementally reads complete JSONL lines, bounded in memory.
// A partial trailing line is held until the writer appends its newline.
type promptTranscript struct {
	io.ReadCloser
	file              io.Reader
	pending           []byte
	input             string
	tool, id          string
	matched, finished bool
	started           time.Time
	initial, current  bool
}

func canonicalPromptInput(raw json.RawMessage) (string, error) {
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		return "", err
	}
	b, err := json.Marshal(v)
	return string(b), err
}
func newPromptTranscript(in PermissionRequest, started time.Time, open func(string) (*os.File, error)) (*promptTranscript, error) {
	f, err := open(in.Transcript)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("invalid transcript")
	}
	start := max(int64(0), info.Size()-promptTranscriptLimit)
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	input, err := canonicalPromptInput(in.Input)
	if err != nil {
		f.Close()
		return nil, err
	}
	w := &promptTranscript{ReadCloser: f, input: input, tool: in.Tool, started: started, initial: true}
	if start > 0 { // Discard the first, possibly truncated, line.
		var one [1]byte
		for {
			n, e := f.Read(one[:])
			if e != nil {
				w.Close()
				return nil, e
			}
			if n > 0 && one[0] == '\n' {
				break
			}
		}
	}
	pos, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		w.Close()
		return nil, err
	}
	// Read only the initial snapshot as history. Appends, including a partial
	// line completed later, belong to the live watcher.
	w.file = io.LimitReader(f, max(0, info.Size()-pos))
	if _, err := w.answered(); err != nil {
		w.Close()
		return nil, err
	}
	if w.finished && !w.current {
		w.id, w.matched, w.finished = "", false, false
	}
	w.file, w.initial = f, false
	return w, nil
}
func (w *promptTranscript) answered() (bool, error) {
	var buf [32 << 10]byte
	total := 0
	for {
		n, err := w.file.Read(buf[:])
		total += n
		if total > 4*promptPayloadLimit {
			return false, errors.New("transcript grew too quickly")
		}
		w.pending = append(w.pending, buf[:n]...)
		for {
			line, rest, ok := bytes.Cut(w.pending, []byte{'\n'})
			if !ok {
				break
			}
			w.line(line)
			w.pending = rest
		}
		if len(w.pending) > promptPayloadLimit {
			return false, errors.New("transcript line too large")
		}
		if err == io.EOF {
			return w.finished, nil
		}
		if err != nil {
			return false, err
		}
	}
}
func (w *promptTranscript) line(data []byte) {
	var line struct {
		Type      string
		Timestamp time.Time
		Message   struct {
			Content []struct {
				Type, ID, Name string
				Input          json.RawMessage
				ToolUseID      string `json:"tool_use_id"`
			}
		}
	}
	if json.Unmarshal(data, &line) != nil {
		return
	}
	for _, block := range line.Message.Content {
		// Bootstrap selects the latest matching use. Once watching live, a
		// subsequent identical command belongs to another permission request.
		if line.Type == "assistant" && block.Type == "tool_use" && block.Name == w.tool && block.ID != "" && (w.initial || !w.matched) {
			input, err := canonicalPromptInput(block.Input)
			if err == nil && input == w.input {
				w.id = block.ID
				w.matched = true
				w.finished = false
				w.current = !w.initial || !line.Timestamp.IsZero() && !line.Timestamp.Before(w.started)
			}
		}
		if line.Type == "user" && block.Type == "tool_result" && w.matched && block.ToolUseID == w.id {
			w.finished = true
		}
	}
}
