package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/chat"
	drigamcp "github.com/labstack/driga/internal/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestChatPromptToolsCapture(t *testing.T) {
	for _, policy := range []string{"none", "metadata", "titles", "snippets"} {
		t.Run(policy, func(t *testing.T) {
			ctx := context.Background()
			called := ""
			o := drigamcp.Options{Chat: true, CaptureText: policy, Prompts: func(context.Context) ([]chat.Prompt, error) {
				return []chat.Prompt{{ID: "one", Session: "session", Tool: "Bash", Input: json.RawMessage(`{"command":"private command","large":9007199254740993}`)}}, nil
			}, Action: func(_ context.Context, name string, raw json.RawMessage) (string, error) {
				called = name
				return "answered", nil
			}}
			a, b := sdk.NewInMemoryTransports()
			ss, err := drigamcp.NewServer(o).Connect(ctx, a, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ss.Close()
			cs, err := sdk.NewClient(&sdk.Implementation{Name: "test"}, nil).Connect(ctx, b, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer cs.Close()
			res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "list_prompts", Arguments: map[string]any{}})
			if err != nil || res.IsError {
				t.Fatal(res, err)
			}
			raw, _ := json.Marshal(res)
			if policy == "snippets" && !strings.Contains(string(raw), "9007199254740993") {
				t.Fatal("prompt input rounded", string(raw))
			}
			if strings.Contains(string(raw), "private command") != (policy == "snippets") || !strings.Contains(string(raw), "Bash") {
				t.Fatal(string(raw))
			}
			res, err = cs.CallTool(ctx, &sdk.CallToolParams{Name: "answer_prompt", Arguments: map[string]any{"id": "one", "allow": false}})
			if err != nil || res.IsError || called != "answer_prompt" {
				t.Fatal(res, err, called)
			}
			called = ""
			res, err = cs.CallTool(ctx, &sdk.CallToolParams{Name: "answer_prompt", Arguments: map[string]any{"id": "one"}})
			if err == nil && !res.IsError || called != "" {
				t.Fatal("missing allow accepted", res, err)
			}
		})
	}
}
