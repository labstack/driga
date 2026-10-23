package chat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writePromptTranscript(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(text)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
}
func TestPromptTranscriptMatching(t *testing.T) {
	for _, tc := range []struct {
		name, tail string
		answered   bool
	}{
		{"wrong input", `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"wrong","name":"Bash","input":{"command":"different"}}]}}` + "\n" + `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"wrong"}]}}` + "\n", false},
		{"result before use", `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"one"}]}}` + "\n" + `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"one","name":"Bash","input":{"b":2,"command":"private"}}]}}` + "\n", false},
		{"canonical input", `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"one","name":"Bash","input":{"b":2,"command":"private"}}]}}` + "\n" + `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"one"}]}}` + "\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "transcript")
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			w, err := newPromptTranscript(PermissionRequest{Transcript: path, Tool: "Bash", Input: json.RawMessage(`{"command":"private","b":2}`)}, time.Now(), os.Open)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			writePromptTranscript(t, path, tc.tail)
			got, err := w.answered()
			if err != nil || got != tc.answered {
				t.Fatal(got, err)
			}
		})
	}
}

func TestPromptTranscriptIgnoresEarlierIdenticalAnsweredCall(t *testing.T) {
	use := func(id string, ts time.Time) string {
		return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"content":[{"type":"tool_use","id":%q,"name":"Bash","input":{"command":"same"}}]}}`+"\n", ts.Format(time.RFC3339Nano), id)
	}
	result := func(id string) string {
		return fmt.Sprintf(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":%q}]}}`+"\n", id)
	}
	for _, scenario := range []string{"earlier answered", "existing unanswered", "answered after hook started"} {
		t.Run(scenario, func(t *testing.T) {
			started := time.Now()
			path := filepath.Join(t.TempDir(), "transcript")
			text := use("old", started.Add(-time.Hour)) + result("old")
			if scenario == "existing unanswered" {
				text += use("new", started.Add(-time.Second))
			} else if scenario == "answered after hook started" {
				text += use("new", started) + result("new")
			}
			if err := os.WriteFile(path, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			w, err := newPromptTranscript(PermissionRequest{Transcript: path, Tool: "Bash", Input: json.RawMessage(`{"command":"same"}`)}, started, os.Open)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			answered, err := w.answered()
			if err != nil || answered != (scenario == "answered after hook started") {
				t.Fatal("historical answer retired current prompt", answered, err)
			}
			if answered {
				return
			}
			if scenario == "earlier answered" {
				writePromptTranscript(t, path, result("old"))
				if answered, err = w.answered(); err != nil || answered {
					t.Fatal("old result matched", answered, err)
				}
				writePromptTranscript(t, path, use("new", started))
			}
			writePromptTranscript(t, path, result("new")+use("next", started.Add(time.Second)))
			if answered, err = w.answered(); err != nil || !answered {
				t.Fatal("new result did not retire prompt", answered, err)
			}
		})
	}
}
