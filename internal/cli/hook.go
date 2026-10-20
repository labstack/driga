package cli

import (
	"bytes"
	"context"
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

	"github.com/labstack/driga/internal/adapter/copilot"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/logfile"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/scrub"
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/spool"
	"github.com/shirou/gopsutil/v4/process"
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
	defer func() {
		if recover() != nil {
			hookLogError(errors.New("hook panic recovered"))
		}
	}()
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
	case "copilot":
		if g == nil || g.Stdin == nil {
			return nil
		}
		data, err = hookReadCopilotInput(g.Stdin)
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
	if h.Adapter == "copilot" {
		return h.appendCopilot(data)
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
	snippets, err := config.HookCaptureSnippets()
	if err != nil {
		hookLogError(err)
		snippets = false
	}
	if ev.Tool == "" && h.Adapter == "claude" && ev.Event == "Notification" && ev.NotificationType == "permission_prompt" {
		tool, input := permissionTranscript(payload.TranscriptPath)
		ev.Tool = scrub.Snippet(tool)
		if tool != "" {
			payload.ToolInput = input
		}
	}
	if snippets {
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
	return hookReadBounded(input, func(r io.Reader) ([]byte, error) {
		data, err := io.ReadAll(io.LimitReader(r, hookPayloadLimit+1))
		if len(data) > hookPayloadLimit {
			return nil, errors.New("hook payload exceeds 1 MiB")
		}
		return data, err
	})
}

func hookReadBounded(input io.Reader, read func(io.Reader) ([]byte, error)) ([]byte, error) {
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
		defer func() {
			if recover() != nil {
				done <- result{err: errors.New("hook stdin panic recovered")}
			}
		}()
		data, err := read(input)
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

// hookParentProcess reads only the direct parent, never the process fleet.
var hookParentProcess = func() (model.Proc, error) {
	pid := int32(os.Getppid())
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	p := &process.Process{Pid: pid}
	exe, exeErr := p.ExeWithContext(ctx)
	cmd, cmdErr := p.CmdlineWithContext(ctx)
	if exeErr != nil && cmdErr != nil {
		return model.Proc{}, errors.New("Copilot parent identity unavailable")
	}
	ms, err := p.CreateTimeWithContext(ctx)
	if err != nil {
		return model.Proc{}, err
	}
	return model.Proc{PID: pid, StartTime: time.UnixMilli(ms), Exe: exe, Cmdline: cmd}, nil
}

var hookParentIdentity = func() (int32, time.Time, error) {
	p, err := hookParentProcess()
	if err != nil {
		return 0, time.Time{}, err
	}
	sigs, err := signatures.LoadAgent("copilot")
	if err != nil {
		return 0, time.Time{}, err
	}
	_, cmdMatch := sigs.Match(p.Cmdline, "")
	_, exeMatch := sigs.Match("", p.Exe)
	if !cmdMatch && !exeMatch {
		return 0, time.Time{}, nil
	}
	return p.PID, p.StartTime, nil
}

func (h HookCmd) appendCopilot(data []byte) error {
	var p struct {
		SessionID        string          `json:"sessionId"`
		SessionIDSnake   string          `json:"session_id"`
		Event            string          `json:"hook_event_name"`
		Timestamp        json.RawMessage `json:"timestamp"`
		Cwd              string          `json:"cwd"`
		Source           string          `json:"source"`
		Reason           string          `json:"reason"`
		StopReason       string          `json:"stopReason"`
		Prompt           string          `json:"prompt"`
		Tool             string          `json:"toolName"`
		ToolArgs         json.RawMessage `json:"toolArgs"`
		Message          string          `json:"message"`
		NotificationType string          `json:"notification_type"`
		Error            struct {
			Name string `json:"name"`
		} `json:"error"`
		Recoverable bool `json:"recoverable"`
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return errors.New("decode Copilot hook payload")
	}

	origin := ""
	if p.SessionIDSnake != "" {
		p.SessionID, origin = p.SessionIDSnake, "vscode"
	}
	if h.Event == "" {
		h.Event = p.Event
	}
	if origin == "vscode" && h.Event != "" {
		// VS Code names events in PascalCase; CLI arguments use lower camel case.
		h.Event = strings.ToLower(h.Event[:1]) + h.Event[1:]
	}
	if !copilot.Event(h.Event) {
		return errors.New("unsupported Copilot hook event")
	}
	var ts time.Time
	var ms int64
	if json.Unmarshal(p.Timestamp, &ms) == nil && ms > 0 {
		ts = time.UnixMilli(ms).UTC()
	} else {
		var stamp string
		if json.Unmarshal(p.Timestamp, &stamp) == nil {
			ts, _ = time.Parse(time.RFC3339Nano, stamp)
		}
	}
	if p.SessionID == "" || ts.IsZero() || p.Cwd == "" {
		return errors.New("Copilot hook missing session id, timestamp or cwd")
	}
	snippets, err := config.HookCaptureSnippets()
	if err != nil {
		hookLogError(err)
		snippets = false
	}
	ev := model.HookEvent{Adapter: "copilot", SessionID: p.SessionID, TS: ts, Origin: origin, Cwd: p.Cwd, Event: h.Event, NotificationType: p.NotificationType, Source: scrub.Snippet(p.Source), Reason: scrub.Snippet(p.Reason)}
	if p.StopReason != "" {
		ev.Reason = scrub.Snippet(p.StopReason)
	}
	if origin != "vscode" {
		ev.PID, ev.ProcStart, err = hookParentIdentity()
		if err != nil {
			hookLogError(errors.New("Copilot parent start time unavailable"))
		}
	}
	if h.Event == "errorOccurred" {
		// Retain only protocol metadata and, under snippets, the error name.
		// Messages, contexts, stacks and unrelated fields never reach disk.
		ev.Source, ev.Reason, ev.NotificationType = "", "", ""
		ev.Recoverable = p.Recoverable
		if snippets && p.Error.Name != "" {
			ev.Reason = scrub.Snippet("error: " + p.Error.Name)
			ev.Message = ev.Reason
		}
		return spool.Append(config.SpoolDir(), ev)
	}
	ev.Tool = scrub.Snippet(p.Tool)
	if h.Event == "notification" {
		ev.Tool = ""
	}
	if snippets {
		ev.Message = scrub.Snippet(p.Message)
		if h.Event == "userPromptSubmitted" {
			ev.Title = scrub.Snippet(p.Prompt)
			ev.Prompt = scrub.Snippet(p.Prompt)
		}
		var input struct {
			Command, Question, Description string
			FilePath                       string `json:"file_path"`
		}
		_ = json.Unmarshal(p.ToolArgs, &input)
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
		ev.Detail = scrub.Snippet(detail)
	}
	return spool.Append(config.SpoolDir(), ev)
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
