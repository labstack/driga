package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/labstack/driga/internal/pathx"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/scrub"
	"github.com/labstack/driga/internal/tail"
)

var filenameID = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.jsonl$`)

type entry struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type metadata struct {
	ID         string          `json:"id"`
	Cwd        string          `json:"cwd"`
	Originator string          `json:"originator"`
	Source     json.RawMessage `json:"source"`
	Version    string          `json:"cli_version"`
	Git        struct {
		Branch string `json:"branch"`
	} `json:"git"`
}

func readRollout(path string, prev model.Offset, snippets bool) (model.SessionObs, []adapter.Warning, model.Offset, error) {
	return readRolloutContext(context.Background(), path, prev, snippets)
}

func readRolloutContext(ctx context.Context, path string, prev model.Offset, snippets bool) (model.SessionObs, []adapter.Warning, model.Offset, error) {
	obs := model.SessionObs{Adapter: "codex", Origin: "interactive", SourcePath: path}
	obs.ExternalID = strings.TrimSuffix(filenameID.FindString(filepath.Base(path)), ".jsonl")
	prev, skipped, err := tail.SeekRecent(path, prev, maxBytes)
	if err != nil {
		return obs, nil, prev, err
	}
	// The collector stores deltas, but relative patch paths and completion IDs
	// need metadata even when this adapter starts with an existing tail offset.
	if prev.Pos > 0 {
		_, err := tail.Scan(ctx, path, model.Offset{}, 1, func(line []byte, _ int64) {
			var e entry
			if json.Unmarshal(line, &e) == nil && e.Type == "session_meta" {
				// Malformed header warnings belong to the pass that consumes it.
				_ = fold(&obs, e, 0, snippets)
			}
		})
		if err != nil {
			return obs, nil, prev, err
		}
	}
	var warnings []adapter.Warning
	if skipped > 0 {
		warnings = append(warnings, adapter.Warning{Kind: "skipped_backlog", Detail: fmt.Sprintf("%s: %d bytes", path, skipped)})
	}
	r, err := tail.Scan(ctx, path, prev, maxBytes, func(line []byte, offset int64) {
		var e entry
		err := json.Unmarshal(line, &e)
		if err == nil {
			err = fold(&obs, e, offset, snippets)
		}
		if err != nil {
			warnings = append(warnings, adapter.Warning{Kind: "invalid_json", Detail: fmt.Sprintf("%s:%d: %v", path, offset, err)})
		}
	})
	if err != nil {
		return obs, warnings, r.Next, err
	}
	for i := 0; i < r.Skipped; i++ {
		warnings = append(warnings, adapter.Warning{Kind: "oversize_line", Detail: path})
	}
	return obs, warnings, r.Next, nil
}

func fold(obs *model.SessionObs, e entry, offset int64, snippets bool) error {
	ts, _ := time.Parse(time.RFC3339Nano, e.Timestamp)
	if ts.After(obs.LastActivityAt) {
		obs.LastActivityAt = ts
	}
	switch e.Type {
	case "session_meta":
		var m metadata
		if err := json.Unmarshal(e.Payload, &m); err != nil {
			return err
		}
		if m.ID != "" {
			obs.ExternalID = m.ID
		}
		obs.Cwd, obs.Version, obs.Branch = m.Cwd, m.Version, m.Git.Branch
		obs.Origin, obs.Hosted = origin(m.Originator, m.Source)
		if temporary(m.Cwd) {
			obs.Origin += "/temporary"
		}
		obs.StartedAt = ts
	case "event_msg":
		var event struct {
			Type             string          `json:"type"`
			Message          json.RawMessage `json:"message"`
			LastAgentMessage json.RawMessage `json:"last_agent_message"`
		}
		if err := json.Unmarshal(e.Payload, &event); err != nil {
			return err
		}
		switch event.Type {
		case "task_started":
			obs.Events = append(obs.Events, model.Event{TS: ts, Kind: "log:turn_start"})
			obs.TurnOpen = true
		case "task_complete", "turn_complete", "turn_completed":
			obs.TurnOpen = false
			obs.Events = append(obs.Events, model.Event{TS: ts, Kind: "log:turn_end"})
			completion := model.CompletionEvent{ID: fmt.Sprintf("codex:%s:%d", obs.ExternalID, offset), TS: ts}
			if snippets {
				completion.Text = capturedText(event.LastAgentMessage)
			}
			obs.Completions = append(obs.Completions, completion)
		case "user_message":
			if ts.After(obs.LastPromptAt) {
				obs.LastPromptAt = ts
				if snippets {
					obs.LastPrompt = capturedText(event.Message)
				}
			}
		}
	case "response_item":
		var tool struct {
			Type   string `json:"type"`
			Name   string `json:"name"`
			CallID string `json:"call_id"`
		}
		if json.Unmarshal(e.Payload, &tool) == nil && (tool.Type == "function_call" || tool.Type == "custom_tool_call") {
			identity := tool.CallID
			if identity == "" {
				identity = fmt.Sprint(offset)
			}
			obs.Events = append(obs.Events, adapter.ToolEvent(ts, tool.Name, identity))
		}
		return patchFiles(obs, e.Payload, ts)
	}
	return nil
}

func capturedText(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return scrub.Snippet(value)
}

func patchFiles(obs *model.SessionObs, payload json.RawMessage, ts time.Time) error {
	var tool struct {
		Type      string          `json:"type"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
		Input     json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(payload, &tool); err != nil {
		return err
	}
	if tool.Name != "apply_patch" || (tool.Type != "function_call" && tool.Type != "custom_tool_call") {
		return nil
	}
	var input string
	if len(tool.Input) > 0 {
		if err := json.Unmarshal(tool.Input, &input); err != nil {
			return err
		}
	}
	if input == "" && len(tool.Arguments) > 0 {
		arguments := tool.Arguments
		if arguments[0] == '"' {
			var decoded string
			if err := json.Unmarshal(arguments, &decoded); err != nil {
				return err
			}
			arguments = json.RawMessage(decoded)
		}
		var args struct {
			Input string `json:"input"`
		}
		if err := json.Unmarshal(arguments, &args); err != nil {
			return err
		}
		input = args.Input
	}
	for _, line := range strings.Split(input, "\n") {
		for _, prefix := range []string{"*** Update File: ", "*** Add File: ", "*** Delete File: ", "*** Move to: "} {
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			path := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			if path == "" {
				continue
			}
			path = pathx.RecordedJoin(obs.Cwd, path)
			obs.FilesTouched = append(obs.FilesTouched, model.FileTouch{Path: path, TS: ts})
			break
		}
	}
	return nil
}

func origin(originator string, source json.RawMessage) (string, bool) {
	value := strings.ToLower(originator + " " + string(source))
	// Exec takes precedence over hosted markers; all other/unknown origins
	// use interactive. Match source strings and object variants alike.
	for _, rule := range []struct{ marker, origin string }{
		{"exec", "headless"},
		{"desktop", "hosted"}, {"app-server", "hosted"},
		{"vscode", "hosted"}, {"plugin", "hosted"}, {"mcp", "hosted"},
	} {
		if strings.Contains(value, rule.marker) {
			return rule.origin, rule.origin == "hosted"
		}
	}
	return "interactive", false
}

func temporary(cwd string) bool { return pathx.TemporaryRecorded(cwd) }
