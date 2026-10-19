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
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/logfile"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/scrub"
	"github.com/labstack/driga/internal/spool"
)

const hookPayloadLimit = 1 << 20

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
		SessionID        string `json:"session_id"`
		ThreadID         string `json:"thread-id"`
		Cwd              string `json:"cwd"`
		Event            string `json:"hook_event_name"`
		Message          string `json:"message"`
		AssistantMessage string `json:"last_assistant_message"`
		CodexMessage     string `json:"last-assistant-message"`
		Type             string `json:"type"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("decode hook payload: %w", err)
	}
	ev := model.HookEvent{TS: time.Now().UTC(), Adapter: h.Adapter, SessionID: payload.SessionID, Cwd: payload.Cwd, Event: h.Event}
	message := payload.Message
	if message == "" {
		message = payload.AssistantMessage
	}
	if h.Adapter == "codex" {
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
	if cfg.CaptureSnippets() {
		ev.Message = scrub.Snippet(message)
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
