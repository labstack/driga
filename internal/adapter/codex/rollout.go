package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	obs := model.SessionObs{Adapter: "codex", Origin: "interactive", SourcePath: path}
	obs.ExternalID = strings.TrimSuffix(filenameID.FindString(filepath.Base(path)), ".jsonl")
	prev, skipped, err := tail.SeekRecent(path, prev, maxBytes)
	if err != nil {
		return obs, nil, prev, err
	}
	r, err := tail.Read(path, prev, maxBytes)
	if err != nil {
		return obs, nil, r.Next, err
	}
	start := prev.Pos
	if r.Reset {
		start = 0
	}
	// The collector stores deltas, but relative patch paths and completion IDs
	// need metadata even when this adapter starts with an existing tail offset.
	if start > 0 {
		header, err := tail.Read(path, model.Offset{}, 1)
		if err != nil {
			return obs, nil, r.Next, err
		}
		if len(header.Lines) > 0 {
			var e entry
			if json.Unmarshal(header.Lines[0], &e) == nil && e.Type == "session_meta" {
				// Malformed header warnings belong to the pass that consumes it.
				_ = fold(&obs, e, 0, snippets)
			}
		}
	}
	positions, err := linePositions(path, start, r)
	if err != nil {
		return obs, nil, r.Next, err
	}
	var warnings []adapter.Warning
	if skipped > 0 {
		warnings = append(warnings, adapter.Warning{Kind: "skipped-backlog", Detail: fmt.Sprintf("%s: %d bytes", path, skipped)})
	}
	for i, line := range r.Lines {
		var e entry
		err := json.Unmarshal(line, &e)
		if err == nil {
			err = fold(&obs, e, positions[i], snippets)
		}
		if err != nil {
			warnings = append(warnings, adapter.Warning{Kind: "invalid-json", Detail: fmt.Sprintf("%s:%d: %v", path, positions[i], err)})
		}
	}
	for i := 0; i < r.Skipped; i++ {
		warnings = append(warnings, adapter.Warning{Kind: "oversize-line", Detail: path})
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
			obs.TurnOpen = true
		case "task_complete", "turn_complete", "turn_completed":
			obs.TurnOpen = false
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
			if !filepath.IsAbs(path) && obs.Cwd != "" {
				path = filepath.Join(obs.Cwd, path)
			}
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

func temporary(cwd string) bool {
	if !filepath.IsAbs(cwd) {
		return false
	}
	cwd = canonicalPath(cwd)
	for _, root := range []string{os.TempDir(), "/tmp", "/private/tmp"} {
		rel, err := filepath.Rel(canonicalPath(root), cwd)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func canonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	// A logged cwd may already be gone. Resolve its existing ancestors
	// while retaining the missing suffix as a clean path.
	path = filepath.Clean(path)
	if parent := filepath.Dir(path); parent != path {
		return filepath.Join(canonicalPath(parent), filepath.Base(path))
	}
	return path
}

// linePositions restores absolute offsets omitted by tail.Result. Usually the
// lengths suffice; skipped oversized records require a bounded boundary scan.
func linePositions(path string, start int64, r tail.Result) ([]int64, error) {
	positions := make([]int64, 0, len(r.Lines))
	pos := start
	for _, line := range r.Lines {
		positions = append(positions, pos)
		pos += int64(len(line)) + 1
	}
	if pos == r.Next.Pos && r.Skipped == 0 {
		return positions, nil
	}
	positions = positions[:0]
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	continuation := false
	if start > 0 {
		var before [1]byte
		if _, err := f.ReadAt(before[:], start-1); err != nil {
			return nil, err
		}
		continuation = before[0] != '\n'
	}
	reader := bufio.NewReaderSize(io.NewSectionReader(f, start, r.Next.Pos-start), 64<<10)
	pos = start
	var size int64
	for {
		fragment, err := reader.ReadSlice('\n')
		size += int64(len(fragment))
		switch err {
		case nil:
			if !continuation && size-1 <= tail.MaxLine {
				positions = append(positions, pos)
			}
			pos += size
			size, continuation = 0, false
		case bufio.ErrBufferFull:
			continue
		case io.EOF:
			if len(positions) != len(r.Lines) {
				return nil, fmt.Errorf("%s: rollout changed while recovering line offsets", path)
			}
			return positions, nil
		default:
			return nil, err
		}
	}
}
