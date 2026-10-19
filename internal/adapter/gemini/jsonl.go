package gemini

import (
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

// Retain only bounded metadata, never transcript bodies or tool results.
const maxMessages = 1024

type toolCall struct {
	id, name, path string
	result         bool
}
type message struct {
	id, kind, snippet string
	ts                time.Time
	tools             []toolCall
}
type conversation struct {
	sessionID, summary string
	started, updated   time.Time
	messages           []message
	truncated          bool
}

type wireTool struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args struct {
		FilePath     string `json:"file_path"`
		AbsolutePath string `json:"absolute_path"`
	} `json:"args"`
	Result json.RawMessage `json:"result"`
}
type wireMessage struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Content   json.RawMessage `json:"content"`
	ToolCalls []wireTool      `json:"toolCalls"`
}

func parseMessage(w wireMessage) message {
	m := message{id: w.ID, kind: w.Type, ts: w.Timestamp}
	// Snippets are scrubbed and bounded before any cache retention.
	var text string
	if json.Unmarshal(w.Content, &text) != nil {
		var parts []struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(w.Content, &parts) == nil {
			for _, p := range parts {
				if len(text) < 1024 {
					text += p.Text
				}
			}
		}
	}
	m.snippet = scrub.Snippet(text)
	for _, t := range w.ToolCalls {
		p := t.Args.FilePath
		if p == "" {
			p = t.Args.AbsolutePath
		}
		m.tools = append(m.tools, toolCall{id: t.ID, name: t.Name, path: p, result: len(t.Result) > 0})
	}
	return m
}

func (f *conversation) put(w wireMessage) {
	if w.ID == "" {
		return
	}
	m := parseMessage(w)
	for i := range f.messages {
		if f.messages[i].id == w.ID {
			f.messages[i] = m
			return
		}
	}
	f.messages = append(f.messages, m)
	if len(f.messages) > maxMessages {
		f.truncated = true
		copy(f.messages, f.messages[len(f.messages)-maxMessages:])
		f.messages = f.messages[:maxMessages]
	}
}

func (f *conversation) metadata(raw json.RawMessage) error {
	var w struct {
		SessionID   *string         `json:"sessionId"`
		Summary     *string         `json:"summary"`
		StartTime   *time.Time      `json:"startTime"`
		LastUpdated *time.Time      `json:"lastUpdated"`
		Messages    json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return err
	}
	if w.SessionID != nil {
		f.sessionID = *w.SessionID
	}
	if w.Summary != nil {
		f.summary = *w.Summary
	}
	if w.StartTime != nil {
		f.started = *w.StartTime
	}
	if w.LastUpdated != nil {
		f.updated = *w.LastUpdated
	}
	if len(w.Messages) > 0 {
		var ms []wireMessage
		if err := json.Unmarshal(w.Messages, &ms); err != nil {
			return err
		}
		f.messages = nil
		f.truncated = false
		for _, m := range ms {
			f.put(m)
		}
	}
	return nil
}

func (f *conversation) record(line []byte) error {
	var r map[string]json.RawMessage
	if err := json.Unmarshal(line, &r); err != nil {
		return err
	}
	// Classification order follows spec 4.1. Control records never become
	// messages merely because they contain an ID inside their payload.
	if r["sessionId"] != nil {
		return f.metadata(line)
	}
	if r["id"] != nil && r["$patch"] == nil {
		var w wireMessage
		if err := json.Unmarshal(line, &w); err != nil {
			return err
		}
		f.put(w)
		return nil
	}
	if raw := r["$set"]; raw != nil {
		return f.metadata(raw)
	}
	if raw := r["$rewindTo"]; raw != nil {
		var id string
		if err := json.Unmarshal(raw, &id); err != nil {
			return err
		}
		for i, m := range f.messages {
			if m.id == id {
				f.messages = f.messages[:i]
				return nil
			}
		}
		f.messages = nil
		return nil
	}
	if raw := r["$patch"]; raw != nil {
		var p struct {
			wireMessage
			Updates   []wireMessage `json:"updates"`
			RemoveIDs []string      `json:"removeIds"`
			OrderIDs  []string      `json:"orderIds"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		for _, update := range append([]wireMessage{p.wireMessage}, p.Updates...) {
			for i := range f.messages {
				m := &f.messages[i]
				if m.id != update.ID {
					continue
				}
				if len(update.Content) > 0 {
					m.snippet = parseMessage(update).snippet
				}
				for _, t := range update.ToolCalls {
					for j := range m.tools {
						if m.tools[j].id == t.ID && len(t.Result) > 0 {
							m.tools[j].result = true
						}
					}
				}
			}
		}
		for _, id := range p.RemoveIDs {
			for i, m := range f.messages {
				if m.id == id {
					f.messages = append(f.messages[:i], f.messages[i+1:]...)
					break
				}
			}
		}
		if len(p.OrderIDs) > 0 {
			ordered := make([]message, 0, len(f.messages))
			selected := map[string]bool{}
			for _, id := range p.OrderIDs {
				selected[id] = true
			}
			for _, m := range f.messages {
				if !selected[m.id] {
					ordered = append(ordered, m)
				}
			}
			seen := map[string]bool{}
			for _, id := range p.OrderIDs {
				if seen[id] {
					continue
				}
				seen[id] = true
				for _, m := range f.messages {
					if m.id == id {
						ordered = append(ordered, m)
						break
					}
				}
			}
			f.messages = ordered
		}
	}
	return nil
}

func (f conversation) observation(path, cwd string, snippets bool) model.SessionObs {
	o := model.SessionObs{Adapter: "gemini", ExternalID: f.sessionID, Origin: "interactive", Title: f.summary, StartedAt: f.started, LastActivityAt: f.updated, SourcePath: path, Cwd: cwd}
	pending := false
	for _, m := range f.messages {
		if m.ts.After(o.LastActivityAt) {
			o.LastActivityAt = m.ts
		}
		if m.kind == "user" {
			pending = false
			o.LastPromptAt = m.ts
			o.TurnOpen = true
			if snippets {
				o.LastPrompt = m.snippet
			}
		}
		if m.kind == "gemini" && !o.LastPromptAt.IsZero() {
			wasOpen := o.TurnOpen
			for _, t := range m.tools {
				pending = pending || !t.result
			}
			o.TurnOpen = pending
			if wasOpen && !pending {
				o.Completions = append(o.Completions, model.CompletionEvent{ID: "gemini:" + f.sessionID + ":" + m.id, TS: m.ts})
			}
		}
		for _, t := range m.tools {
			if (t.name != "write_file" && t.name != "replace" && t.name != "edit") || t.path == "" {
				continue
			}
			p := t.path
			if !filepath.IsAbs(p) {
				if cwd == "" {
					continue
				}
				p = filepath.Join(cwd, p)
			}
			o.FilesTouched = append(o.FilesTouched, model.FileTouch{Path: filepath.Clean(p), TS: m.ts})
		}
	}
	return o
}

func (a *geminiAdapter) readJSONL(ctx context.Context, path string, prev model.Offset) (conversation, model.Offset, []adapter.Warning, error) {
	c, ok := a.cache[path]
	// A fresh process or rolled-back collector offset needs a fresh fold.
	// Compare file identity, not the struct: stored offsets carry a machine ID
	// and a UTC mtime that the tail's own value never has.
	if !ok || c.next.Inode != prev.Inode || c.next.Size != prev.Size || c.next.Pos != prev.Pos || !c.next.Mtime.Equal(prev.Mtime) {
		c = cachedFile{}
		prev = model.Offset{}
	}
	snapshot, err := tail.Read(path, prev, 0)
	if err != nil {
		return conversation{}, prev, nil, err
	}
	if snapshot.Reset {
		c.fold = conversation{}
		prev = model.Offset{}
	}
	start, skipped, err := tail.SeekRecent(path, prev, maxBytes)
	if err != nil {
		return conversation{}, prev, nil, err
	}
	var warnings []adapter.Warning
	if skipped > 0 {
		c.fold = conversation{}
		warnings = append(warnings, adapter.Warning{Kind: "skipped-backlog", Detail: fmt.Sprintf("%s: %d bytes; fold covers retained tail", path, skipped)})
		_, err = tail.Scan(ctx, path, model.Offset{}, 1, func(line []byte, _ int64) { _ = c.fold.record(line) })
		if err != nil {
			return conversation{}, prev, warnings, err
		}
	}
	result, err := tail.Scan(ctx, path, start, maxBytes, func(line []byte, pos int64) {
		if err := c.fold.record(line); err != nil {
			warnings = append(warnings, adapter.Warning{Kind: "invalid-json", Detail: fmt.Sprintf("%s:%d", path, pos)})
		}
	})
	if err != nil {
		return conversation{}, result.Next, warnings, err
	}
	if result.Reset && start.Pos > 0 { // Recovery rename between the stat and scan.
		delete(a.cache, path)
		return a.readJSONL(ctx, path, model.Offset{})
	}
	if result.Skipped > 0 {
		warnings = append(warnings, adapter.Warning{Kind: "oversize-line", Detail: path})
	}
	if c.fold.truncated {
		warnings = append(warnings, adapter.Warning{Kind: "skipped-backlog", Detail: path + ": fold retains the last 1024 messages"})
	}
	c.next = result.Next
	a.remember(path, c)
	return c.fold, result.Next, warnings, nil
}
