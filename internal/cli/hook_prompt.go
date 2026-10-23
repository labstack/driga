package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/config"
)

type permissionPayload struct {
	Session    string          `json:"session_id"`
	Cwd        string          `json:"cwd"`
	Transcript string          `json:"transcript_path"`
	Tool       string          `json:"tool_name"`
	Input      json.RawMessage `json:"tool_input"`
}

func promptBroker() (*chat.Remote, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	return chat.Dial(ctx, config.DataDir())
}
func endPromptSession(session, cwd string) {
	c, err := promptBroker()
	if err != nil {
		return
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = c.EndPrompts(ctx, session, cwd)
}
func (h HookCmd) permission(g *Globals) error {
	if g == nil || g.Stdin == nil || g.Stdout == nil {
		return nil
	}
	// Dial first so an absent collector never waits for stdin or transcript IO.
	c, err := promptBroker()
	if err != nil {
		return nil
	}
	defer c.Close()
	data, err := hookReadInput(g.Stdin)
	if err != nil {
		return nil
	}
	var in permissionPayload
	if json.Unmarshal(data, &in) != nil || in.Session == "" || in.Tool == "" || !json.Valid(in.Input) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), chat.ApprovalTTL)
	defer cancel()
	watch, err := newPromptTranscript(in)
	if err != nil {
		return nil
	}
	defer watch.Close()
	answered, err := watch.answered()
	if err != nil || answered {
		return nil
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
	out, err := c.WaitPrompt(ctx, chat.Prompt{ID: uuid.NewString(), Session: in.Session, Cwd: in.Cwd, Tool: in.Tool, Input: in.Input})
	stopWatch()
	if err != nil || ctx.Err() != nil || (out.Behavior != "allow" && out.Behavior != "deny") {
		return nil
	}
	// The watcher is stopped; retain its matched tool id for the final check,
	// even if the tool_use has since fallen outside the bounded tail.
	answered, err = watch.answered()
	if err != nil || answered || ctx.Err() != nil {
		return nil
	}
	_ = json.NewEncoder(g.Stdout).Encode(struct {
		Output any `json:"hookSpecificOutput"`
	}{struct {
		Event    string              `json:"hookEventName"`
		Decision chat.PromptDecision `json:"decision"`
	}{"PermissionRequest", out}})
	return nil
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
func newPromptTranscript(in permissionPayload) (*promptTranscript, error) {
	f, err := openTranscript(in.Transcript)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("invalid transcript")
	}
	start := max(int64(0), info.Size()-hookTranscriptLimit)
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	input, err := canonicalPromptInput(in.Input)
	if err != nil {
		f.Close()
		return nil, err
	}
	w := &promptTranscript{ReadCloser: f, file: f, input: input, tool: in.Tool}
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
	return w, nil
}
func (w *promptTranscript) answered() (bool, error) {
	var buf [32 << 10]byte
	total := 0
	for {
		n, err := w.file.Read(buf[:])
		total += n
		if total > 4*hookPayloadLimit {
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
		if len(w.pending) > hookPayloadLimit {
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
		Type    string
		Message struct {
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
		if line.Type == "assistant" && block.Type == "tool_use" && block.Name == w.tool && block.ID != "" {
			input, err := canonicalPromptInput(block.Input)
			if err == nil && input == w.input {
				w.id = block.ID
				w.matched = true
				w.finished = false
			}
		}
		if line.Type == "user" && block.Type == "tool_result" && w.matched && block.ToolUseID == w.id {
			w.finished = true
		}
	}
}
