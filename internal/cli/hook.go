package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/logfile"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/scrub"
	"github.com/labstack/driga/internal/spool"
)

const hookPayloadLimit = 1 << 20
const hookTranscriptLimit = 256 << 10

// HookCmd captures an agent hook quickly, without the store or process scans.
type HookCmd struct {
	Adapter string   `arg:""`
	Event   string   `arg:""`
	Payload []string `arg:"" optional:""`
}

// Run always succeeds so failures cannot interrupt the invoking agent. Errors
// are recorded privately in the spool directory without including raw payloads.
func (h HookCmd) Run(g *Globals) error {
	if os.Getenv("DRIGA_INTERNAL") == "1" {
		return nil
	}
	if err := h.append(g); err != nil {
		hookLogError(err)
	}
	return nil
}

func (h HookCmd) append(g *Globals) error {
	var data []byte
	var err error
	switch h.Adapter {
	case "claude":
		if g == nil || g.Stdin == nil {
			return nil
		}
		data, err = hookReadInput(g.Stdin)
	case "codex":
		if h.Event != "notify" {
			if g == nil || g.Stdin == nil {
				return nil
			}
			data, err = hookReadInput(g.Stdin)
			break
		}
		if len(h.Payload) == 0 {
			return errors.New("codex hook has no notify payload")
		}
		payload := h.Payload[len(h.Payload)-1]
		if len(payload) > hookPayloadLimit {
			return errors.New("hook payload exceeds 1 MiB")
		}
		data = []byte(payload)
	default:
		return errors.New("unsupported hook adapter")
	}
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	var payload struct {
		SessionID        string          `json:"session_id"`
		ThreadID         string          `json:"thread-id"`
		Cwd              string          `json:"cwd"`
		Event            string          `json:"hook_event_name"`
		Message          string          `json:"message"`
		AssistantMessage string          `json:"last_assistant_message"`
		CodexMessage     string          `json:"last-assistant-message"`
		Type             string          `json:"type"`
		NotificationType string          `json:"notification_type"`
		TranscriptPath   string          `json:"transcript_path"`
		Tool             string          `json:"tool_name"`
		ToolInput        json.RawMessage `json:"tool_input"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("decode hook payload: %w", err)
	}
	ev := model.HookEvent{TS: time.Now().UTC(), Adapter: h.Adapter, SessionID: payload.SessionID, Cwd: payload.Cwd, Event: h.Event}
	ev.NotificationType, ev.Tool = payload.NotificationType, scrub.Snippet(payload.Tool)
	if ev.Tool == "" && ev.NotificationType == "permission_prompt" {
		ev.Tool = scrub.Snippet(permissionTool(payload.Message))
	}
	message := payload.Message
	if message == "" {
		message = payload.AssistantMessage
	}
	if h.Adapter == "codex" && h.Event == "notify" {
		ev.SessionID = payload.ThreadID
		message = payload.CodexMessage
	}
	if ev.SessionID == "" {
		return errors.New("hook payload has no session id")
	}
	if ev.Event == "" {
		ev.Event = payload.Event
	}
	cfg, err := config.Load(config.Path())
	if err != nil {
		hookLogError(err)
		cfg = config.Default()
	}
	if ev.Tool == "" && h.Adapter == "claude" && ev.Event == "Notification" && ev.NotificationType == "permission_prompt" {
		tool, input := permissionTranscript(payload.TranscriptPath)
		ev.Tool = scrub.Snippet(tool)
		if tool != "" {
			payload.ToolInput = input
		}
	}
	if cfg.CaptureSnippets() {
		ev.Message = scrub.Snippet(message)
		var input struct {
			Command     string `json:"command"`
			Question    string `json:"question"`
			Description string `json:"description"`
			FilePath    string `json:"file_path"`
		}
		// MCP tools may send any JSON value. Opaque input cannot suppress the
		// metadata-only permission event or manufacture an approval decision.
		_ = json.Unmarshal(payload.ToolInput, &input)
		detail := input.Command
		if detail == "" {
			detail = input.Question
		}
		if detail == "" {
			detail = input.Description
		}
		if detail == "" {
			detail = input.FilePath
		}
		if detail == "" {
			detail = message
		}
		ev.Detail = scrub.Snippet(detail)
	}
	return spool.Append(config.SpoolDir(), ev)
}

func hookReadInput(input io.Reader) ([]byte, error) {
	if f, ok := input.(*os.File); ok {
		info, err := f.Stat()
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeCharDevice != 0 {
			return nil, nil
		}
	}
	type result struct {
		data []byte
		err  error
	}
	done := make(chan result, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(input, hookPayloadLimit))
		done <- result{data, err}
	}()
	// A hook can inherit an open pipe without a payload or EOF. Do not wait
	// forever for its writer. The buffered result lets a late read finish even
	// after this short-lived hook process has decided to return. Allow slow
	// writers on loaded machines time to deliver their payload.
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case r := <-done:
		if r.err != nil {
			return nil, r.err
		}
		return r.data, nil
	case <-timer.C:
		return nil, errors.New("hook stdin timed out")
	}
}

func hookLogError(err error) {
	w := &logfile.Writer{Path: filepath.Join(config.SpoolDir(), "hook-errors.log")}
	slog.New(logfile.TextHandler(w)).Error("hook capture failed", "event", "hook_failed", "err", scrub.Snippet(err.Error()))
}

// permissionTool reads the tool name from Claude's older permission message
// ("Claude needs your permission to use Bash").
func permissionTool(message string) string {
	const marker = "needs your permission to use "
	i := strings.Index(message, marker)
	if i < 0 {
		return ""
	}
	name, _, _ := strings.Cut(strings.TrimSpace(message[i+len(marker):]), " ")
	if name == "" || strings.ContainsAny(name, "/\\\"'`$") {
		return ""
	}
	return name
}

// permissionTranscript finds the last tool use without a later result in a
// bounded tail. Transcript failures must not interfere with the agent's hook.
func permissionTranscript(path string) (string, json.RawMessage) {
	// Nonblocking open also handles symlinks to FIFOs without waiting for a writer.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", nil
	}
	start := max(int64(0), info.Size()-hookTranscriptLimit)
	data, err := io.ReadAll(io.NewSectionReader(f, start, min(info.Size(), hookTranscriptLimit)))
	if err != nil {
		return "", nil
	}
	if start > 0 {
		_, data, _ = bytes.Cut(data, []byte{'\n'})
	}
	lines := bytes.Split(data, []byte{'\n'})
	answered := map[string]bool{}
	for i := len(lines) - 1; i >= 0; i-- {
		var line struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type      string          `json:"type"`
					ID        string          `json:"id"`
					Name      string          `json:"name"`
					Input     json.RawMessage `json:"input"`
					ToolUseID string          `json:"tool_use_id"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(lines[i], &line) != nil {
			continue
		}
		for j := len(line.Message.Content) - 1; j >= 0; j-- {
			block := line.Message.Content[j]
			if line.Type == "user" && block.Type == "tool_result" {
				answered[block.ToolUseID] = true
			}
			if line.Type == "assistant" && block.Type == "tool_use" && block.ID != "" && block.Name != "" && !answered[block.ID] {
				return block.Name, block.Input
			}
		}
	}
	return "", nil
}
