package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestChatEvidenceCaptureBoundary(t *testing.T) {
	for _, policy := range []string{"titles", "snippets"} {
		data := map[string]any{"id": json.Number("9007199254740993"), "path": "/fixture/project", "title": "Fixture title", "last_prompt": "private ghp_example", "events": []any{map[string]any{"summary": strings.Repeat("x", 400)}}, "cmdline": "claude -p private", "top_memory": []any{map[string]any{"processes": []any{map[string]any{"argv": []any{"claude", "private ghp_example", strings.Repeat("x", 400)}}}}}, "handoffs": []any{map[string]any{"text": "private handoff"}}}
		out := captureEvidence(data, policy).(map[string]any)
		if out["path"] != "/fixture/project" || out["id"] != json.Number("9007199254740993") || out["title"] != "Fixture title" {
			t.Fatal(out)
		}
		raw, _ := json.Marshal(out)
		if strings.Contains(string(raw), "ghp_example") {
			t.Fatal(string(raw))
		}
		if policy == "titles" {
			if strings.Contains(string(raw), "private") {
				t.Fatal(string(raw))
			}
		} else {
			if len([]rune(out["top_memory"].([]any)[0].(map[string]any)["processes"].([]any)[0].(map[string]any)["argv"].([]any)[2].(string))) > 200 {
				t.Fatal(out)
			}
			if len([]rune(out["events"].([]any)[0].(map[string]any)["summary"].(string))) > 200 {
				t.Fatal(out)
			}
		}
	}
}
