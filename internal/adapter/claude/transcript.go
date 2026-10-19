package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/scrub"
	"github.com/labstack/driga/internal/tail"
)

var maxTailBytes int64 = 8 << 20

type transcriptEntry struct {
	Type       string          `json:"type"`
	Cwd        string          `json:"cwd"`
	GitBranch  string          `json:"gitBranch"`
	Timestamp  string          `json:"timestamp"`
	SessionID  string          `json:"sessionId"`
	IsMeta     bool            `json:"isMeta"`
	Message    json.RawMessage `json:"message"`
	AITitle    string          `json:"aiTitle"`
	Title      string          `json:"title"`
	LastPrompt string          `json:"lastPrompt"`
	Prompt     string          `json:"prompt"`
	StopReason json.RawMessage `json:"stopReason"`
}

type message struct {
	Content    json.RawMessage `json:"content"`
	StopReason string          `json:"stop_reason"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func tailTranscript(ctx context.Context, path string, o *model.SessionObs, env adapter.Env) ([]adapter.Warning, error) {
	var prev model.Offset
	if env.Offsets != nil {
		var err error
		prev, _, err = env.Offsets.GetOffset(path)
		if err != nil {
			return nil, fmt.Errorf("get offset %s: %w", path, err)
		}
	}
	prev, skipped, err := tail.SeekRecent(path, prev, maxTailBytes)
	if err != nil {
		return nil, fmt.Errorf("seek recent %s: %w", path, err)
	}
	var warnings []adapter.Warning
	if skipped > 0 {
		warnings = append(warnings, adapter.Warning{Kind: "skipped_backlog", Detail: fmt.Sprintf("%s: %d bytes", path, skipped)})
		if err := readTranscriptIdentity(ctx, path, o, prev.Pos); err != nil {
			return warnings, err
		}
	}
	// Each collection starts with only the registry snapshot. Read resets its
	// position on truncation or rotation, so a reset folds from scratch too.
	invalid := 0
	r, err := tail.Scan(ctx, path, prev, maxTailBytes, func(line []byte, _ int64) {
		var e transcriptEntry
		if err := json.Unmarshal(line, &e); err != nil {
			invalid++
			return
		}
		foldEntry(o, e, env.CaptureSnippets)
	})
	if err != nil {
		return warnings, fmt.Errorf("tail %s: %w", path, err)
	}
	if env.Offsets != nil {
		if err := env.Offsets.SetOffset(path, r.Next); err != nil {
			return nil, fmt.Errorf("set offset %s: %w", path, err)
		}
	}
	if invalid > 0 {
		warnings = append(warnings, adapter.Warning{Kind: "invalid_json", Detail: fmt.Sprintf("%s: %d lines", path, invalid)})
	}
	if r.Skipped > 0 {
		warnings = append(warnings, adapter.Warning{Kind: "oversize_line", Detail: fmt.Sprintf("%s: %d lines", path, r.Skipped)})
	}
	return warnings, nil
}

// Recover identity from a bounded head read without replaying stale activity,
// prompts or edits when the fold starts near EOF.
func readTranscriptIdentity(ctx context.Context, path string, o *model.SessionObs, end int64) error {
	_, err := tail.Scan(ctx, path, model.Offset{}, min(end, 64<<10), func(line []byte, _ int64) {
		var e transcriptEntry
		if json.Unmarshal(line, &e) != nil {
			return
		}
		if e.SessionID != "" {
			o.ExternalID = e.SessionID
		}
		if e.Cwd != "" {
			o.Cwd = e.Cwd
		}
		if e.GitBranch != "" {
			o.Branch = e.GitBranch
		}
	})
	if err != nil {
		return fmt.Errorf("transcript header %s: %w", path, err)
	}
	return nil
}

func foldEntry(o *model.SessionObs, e transcriptEntry, snippets bool) {
	if e.Cwd != "" {
		o.Cwd = e.Cwd
	}
	if e.GitBranch != "" {
		o.Branch = e.GitBranch
	}
	ts, _ := time.Parse(time.RFC3339Nano, e.Timestamp)
	if ts.After(o.LastActivityAt) {
		o.LastActivityAt = ts
	}
	switch e.Type {
	case "ai-title":
		o.Title = e.AITitle
		if o.Title == "" {
			o.Title = e.Title
		}
	case "last-prompt":
		if snippets {
			prompt := e.LastPrompt
			if prompt == "" {
				prompt = e.Prompt
			}
			o.LastPrompt = scrub.Snippet(prompt)
		}
	case "user", "assistant":
		var m message
		if json.Unmarshal(e.Message, &m) != nil {
			return
		}
		if e.Type == "user" {
			if !e.IsMeta && realPrompt(m.Content) {
				o.TurnOpen = true
				if ts.After(o.LastPromptAt) {
					o.LastPromptAt = ts
				}
			}
			return
		}
		var blocks []contentBlock
		if json.Unmarshal(m.Content, &blocks) == nil {
			for _, block := range blocks {
				foldTool(o, block, ts)
			}
		}
		if m.StopReason == "end_turn" {
			o.TurnOpen = false
		}
	case "system":
		if len(e.StopReason) > 0 && !bytes.Equal(e.StopReason, []byte("null")) {
			o.TurnOpen = false
		}
	}
}

func realPrompt(content json.RawMessage) bool {
	if len(content) == 0 {
		return false
	}
	if content[0] == '"' {
		return true
	}
	var blocks []contentBlock
	if json.Unmarshal(content, &blocks) != nil {
		return false
	}
	for _, block := range blocks {
		if block.Type == "text" {
			return true
		}
	}
	return false
}

func foldTool(o *model.SessionObs, block contentBlock, ts time.Time) {
	if block.Type != "tool_use" {
		return
	}
	switch block.Name {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
	default:
		return
	}
	var input struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	}
	if json.Unmarshal(block.Input, &input) != nil {
		return
	}
	path := input.FilePath
	if path == "" {
		path = input.NotebookPath
	}
	if path == "" {
		return
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(o.Cwd, path)
	}
	o.FilesTouched = append(o.FilesTouched, model.FileTouch{Path: path, TS: ts})
}
