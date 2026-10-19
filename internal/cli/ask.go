package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/llm"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

type AskCmd struct {
	Question []string `arg:"" required:"" help:"Question to answer using Driga's read-only MCP tools."`
}

func (a AskCmd) Run(g *Globals) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := llm.Probe(ctx)
	if err != nil {
		return err
	}
	if err = c.Require("p", "setting-sources", "strict-mcp-config", "mcp-config", "tools", "allowedTools", "no-session-persistence", "name", "output-format", "verbose"); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Join(config.DataDir(), "tmp")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "ask-mcp-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	conf := map[string]any{"mcpServers": map[string]any{"driga": map[string]any{"command": executable, "args": []string{"mcp"}}}}
	encodeErr := json.NewEncoder(f).Encode(conf)
	if err := errors.Join(encodeErr, f.Close()); err != nil {
		return err
	}
	args := []string{"-p", "--setting-sources", "", "--strict-mcp-config", "--mcp-config", f.Name(), "--tools", "", "--allowedTools", "mcp__driga__*", "--no-session-persistence", "--name", "driga-internal", "--output-format", "stream-json", "--verbose"}
	output := io.Writer(io.Discard)
	if !g.JSON {
		output = g.Stdout
	}
	stream := askStream{out: output, cancel: cancel}
	runErr := c.RunTo(ctx, args, strings.Join(a.Question, " "), &stream)
	if stream.err != nil {
		return stream.err
	}
	if runErr != nil {
		return runErr
	}
	if err := stream.finish(); err != nil {
		return err
	}
	if g.JSON {
		r, mode, err := decideMode(ctx, g)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		envelope := view.Envelope{Schema: 1, ObservedAt: now, Mode: mode, Freshness: map[string]view.Fresh{}, CoverageGaps: []model.Gap{}, Data: stream.result}
		if r != nil {
			defer r.Close()
			envelope, err = view.NewEnvelope(r, mode, now, collector.Cadence(0), stream.result)
			if err != nil {
				return err
			}
		}
		return json.NewEncoder(g.Stdout).Encode(envelope)
	}
	_, err = fmt.Fprintln(g.Stdout)
	return err
}

const askMaxLine = 8 << 20

// askStream consumes complete JSONL objects directly from the child's stdout.
// Result output is retained for --json; assistant text is delivered immediately.
type askStream struct {
	out                io.Writer
	cancel             context.CancelFunc
	pending            []byte
	result             json.RawMessage
	connected, printed bool
	err                error
}

func (s *askStream) Write(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	n := len(p)
	for len(p) > 0 {
		line, rest, found := bytes.Cut(p, []byte{'\n'})
		if len(s.pending)+len(line) > askMaxLine {
			return 0, s.fail(fmt.Errorf("bad claude output: stream line exceeds limit"))
		}
		s.pending = append(s.pending, line...)
		if !found {
			break
		}
		if err := s.line(s.pending); err != nil {
			return 0, s.fail(err)
		}
		s.pending = s.pending[:0]
		p = rest
	}
	return n, nil
}

func (s *askStream) fail(err error) error {
	s.err = err
	// Stop only the Claude child started for this request if output is invalid.
	s.cancel()
	return err
}

func (s *askStream) line(line []byte) error {
	if len(bytes.TrimSpace(line)) == 0 {
		return nil
	}
	var event struct {
		Type       string                          `json:"type"`
		Subtype    string                          `json:"subtype"`
		MCPServers []struct{ Name, Status string } `json:"mcp_servers"`
		Message    struct {
			Content []struct{ Type, Text string } `json:"content"`
		} `json:"message"`
		Result  *string `json:"result"`
		IsError bool    `json:"is_error"`
	}
	if err := json.Unmarshal(line, &event); err != nil || event.Type == "" {
		return fmt.Errorf("bad claude output: expected stream-json object")
	}
	if s.result != nil {
		return fmt.Errorf("bad claude output: event after final result")
	}
	switch event.Type {
	case "system":
		if event.Subtype == "init" {
			for _, server := range event.MCPServers {
				if server.Name == "driga" && server.Status == "connected" {
					s.connected = true
				}
			}
			if !s.connected {
				return fmt.Errorf("driga MCP server did not connect under --setting-sources empty; refusing ask")
			}
		}
	case "assistant":
		if !s.connected {
			return fmt.Errorf("driga MCP server connection was not confirmed")
		}
		for _, content := range event.Message.Content {
			if content.Type == "text" && content.Text != "" {
				if _, err := io.WriteString(s.out, content.Text); err != nil {
					return err
				}
				s.printed = true
			}
		}
	case "result":
		if !s.connected {
			return fmt.Errorf("driga MCP server connection was not confirmed")
		}
		if event.IsError {
			return fmt.Errorf("bad claude output: final result reports an error")
		}
		if event.Result == nil {
			return fmt.Errorf("bad claude output: missing result text")
		}
		s.result = append(json.RawMessage(nil), line...)
		if !s.printed {
			if _, err := io.WriteString(s.out, *event.Result); err != nil {
				return err
			}
			s.printed = true
		}
	case "user", "stream_event", "tool_progress", "tool_use_summary", "rate_limit_event":
		// Protocol metadata and tool traffic never become assistant text.
	default:
		return fmt.Errorf("bad claude output: unknown stream type %q", event.Type)
	}
	return nil
}

func (s *askStream) finish() error {
	if len(bytes.TrimSpace(s.pending)) > 0 {
		if err := s.line(s.pending); err != nil {
			return err
		}
	}
	if s.result == nil {
		return fmt.Errorf("bad claude output: missing final result")
	}
	return nil
}
