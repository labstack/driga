package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
)

func TestClaudePermissionToolFromTranscript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink fixture; Windows watch tests require no Developer Mode or admin rights")
	}

	bash := `{"type":"tool_use","id":"bash","name":"Bash","input":{"command":"echo ghp_secret"}}`
	edit := `{"type":"tool_use","id":"edit","name":"Edit","input":{"file_path":"/repo/file.go"}}`
	assistant := func(blocks ...string) string {
		return `{"type":"assistant","message":{"content":[` + strings.Join(blocks, ",") + "]}}\n"
	}
	result := `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"bash","content":"done"}]}}` + "\n"
	for _, tc := range []struct {
		name, transcript, message, notification, adapter, tool, policy string
		toolInput                                                      string
		wantTool, wantDetail                                           string
		missing, symlink                                               bool
	}{
		{name: "pending Bash", transcript: assistant(bash), wantTool: "Bash"},
		{name: "answered", transcript: assistant(bash) + result},
		{name: "last pending", transcript: assistant(bash) + assistant(edit), wantTool: "Edit"},
		{name: "last block", transcript: assistant(bash, edit), wantTool: "Edit"},
		{name: "last answered", transcript: assistant(edit) + assistant(bash) + result, wantTool: "Edit"},
		{name: "result before use", transcript: result + assistant(bash), wantTool: "Bash"},
		{name: "malformed lines", transcript: "invalid JSON\n" + assistant(bash) + "{\"type\":", wantTool: "Bash"},
		{name: "string content", transcript: assistant(bash) + `{"type":"user","message":{"content":"hello"}}` + "\n", wantTool: "Bash"},
		{name: "truncated first line", transcript: strings.Repeat("x", 256<<10) + "\n" + assistant(bash), wantTool: "Bash"},
		{name: "outside tail", transcript: assistant(edit) + strings.Repeat("x", 256<<10) + "\n"},
		{name: "symlink tail", transcript: strings.Repeat("x", 256<<10) + "\n" + assistant(bash), symlink: true, wantTool: "Bash"},
		{name: "missing", missing: true},
		{name: "missing input stays private", missing: true, policy: "snippets", toolInput: `{"command":"echo safe"}`},
		{name: "message wins", transcript: assistant(edit), message: "Claude needs your permission to use Bash", policy: "snippets", wantTool: "Bash"},
		{name: "payload wins", transcript: assistant(edit), tool: "Read", wantTool: "Read"},
		{name: "other notification", transcript: assistant(bash), notification: "idle_prompt"},
		{name: "other adapter", transcript: assistant(bash), adapter: "codex"},
		{name: "titles", transcript: assistant(bash), policy: "titles", wantTool: "Bash"},
		{name: "snippets command", transcript: assistant(bash), policy: "snippets", wantTool: "Bash"},
		{name: "snippets path", transcript: assistant(edit), policy: "snippets", wantTool: "Edit"},
		{name: "snippets Write", transcript: assistant(`{"type":"tool_use","id":"write","name":"Write","input":{"file_path":"/repo/new.go","content":"private"}}`), policy: "snippets", wantTool: "Write"},
		{name: "snippets Read", transcript: assistant(`{"type":"tool_use","id":"read","name":"Read","input":{"file_path":"/repo/read.go"}}`), policy: "snippets", wantTool: "Read"},
		{name: "MCP opaque input", transcript: assistant(`{"type":"tool_use","id":"mcp","name":"mcp__x__y","input":["opaque",1]}`), wantTool: "mcp__x__y"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := hookTestEnv(t)
			policy := tc.policy
			if policy == "" {
				policy = "titles"
			}
			if err := os.WriteFile(os.Getenv("DRIGA_CONFIG"), []byte("capture_text = '"+policy+"'\n"), 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "transcript.jsonl")
			if !tc.missing {
				if err := os.WriteFile(path, []byte(tc.transcript), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.symlink {
				link := path + ".link"
				if err := os.Symlink(path, link); err != nil {
					t.Fatal(err)
				}
				path = link
			}
			message, notification, adapter := tc.message, tc.notification, tc.adapter
			if message == "" {
				message = "Claude needs your permission"
			}
			if notification == "" {
				notification = "permission_prompt"
			}
			if adapter == "" {
				adapter = "claude"
			}
			fields := map[string]any{"session_id": "s", "notification_type": notification, "message": message, "transcript_path": path, "tool_name": tc.tool}
			if tc.toolInput != "" {
				fields["tool_input"] = json.RawMessage(tc.toolInput)
			}
			payload, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			if err := (HookCmd{Adapter: adapter, Event: "Notification"}).Run(&Globals{Stdin: bytes.NewReader(payload)}); err != nil {
				t.Fatal(err)
			}
			got, _, err := spool.Drain(dir, nil)
			if err != nil || len(got) != 1 {
				t.Fatal(got, err)
			}
			ev := got[0]
			if ev.Tool != tc.wantTool || ev.Detail != tc.wantDetail || ev.NotificationType != notification {
				t.Fatalf("event: %+v; want tool %q, detail %q", ev, tc.wantTool, tc.wantDetail)
			}
			if policy == "titles" && ev.Message != "" {
				t.Fatalf("titles captured message: %+v", ev)
			}
			if tc.name == "titles" {
				_, data := feed.Encode(feed.Inbox, model.InboxItem{ID: "wait", Kind: "waiting", Reason: "permission", Tool: ev.Tool, Detail: ev.Detail}, feed.None)
				var item model.InboxItem
				if err := json.Unmarshal(data, &item); err != nil {
					t.Fatal(err)
				}
				if item.Tool != "" || item.Detail != "" || item.Reason != "permission" {
					t.Fatalf("fleet none policy: %s", data)
				}
			}
		})
	}
}

func TestClaudePermissionTranscriptBoundsText(t *testing.T) {
	dir := hookTestEnv(t)
	if err := os.WriteFile(os.Getenv("DRIGA_CONFIG"), []byte("capture_text = 'snippets'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"long","name":"` + strings.Repeat("x", 300) + `","input":{"command":"ghp_secret ` + strings.Repeat("界", 300) + `"}}]}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]string{"session_id": "s", "notification_type": "permission_prompt", "transcript_path": path})
	if err := (HookCmd{Adapter: "claude", Event: "Notification"}).Run(&Globals{Stdin: bytes.NewReader(payload)}); err != nil {
		t.Fatal(err)
	}
	events, _, err := spool.Drain(dir, nil)
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
	if ev := events[0]; utf8.RuneCountInString(ev.Tool) != 200 || ev.Detail != "" || ev.Message != "" {
		t.Fatalf("unbounded or unscrubbed text: %+v", ev)
	}
}
