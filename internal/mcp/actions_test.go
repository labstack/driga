package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	drigamcp "github.com/labstack/driga/internal/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestChatToolsOnlyWithChat(t *testing.T) {
	for _, chat := range []bool{false, true} {
		t.Run(map[bool]string{true: "chat", false: "read-only"}[chat], func(t *testing.T) {
			called := ""
			var input json.RawMessage
			o := drigamcp.Options{Chat: chat, Action: func(_ context.Context, name string, raw json.RawMessage) (string, error) {
				called = name
				input = raw
				return "done", nil
			}, Approve: func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
				input = raw
				return json.RawMessage(`{"behavior":"deny","message":"user denied"}`), nil
			}}
			ctx := context.Background()
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
			list, err := cs.ListTools(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"answer_prompt", "ack_inbox", "jump", "kill", "cleanup", "summarize", "approve"} {
				found := false
				for _, tool := range list.Tools {
					if tool.Name == name {
						found = true
					}
				}
				if found != chat {
					t.Fatalf("%s registered=%v", name, found)
				}
			}
			if !chat {
				return
			}
			for _, tc := range []struct {
				name string
				args map[string]any
			}{
				{"ack_inbox", map[string]any{"id": "one"}}, {"jump", map[string]any{"session_id": 7}}, {"kill", map[string]any{"session_id": 7, "force": true}}, {"cleanup", map[string]any{"id": "candidate"}}, {"summarize", map[string]any{"project": "api"}},
			} {
				res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: tc.name, Arguments: tc.args})
				if err != nil || res.IsError || called != tc.name || !json.Valid(input) {
					t.Fatal(tc.name, res, err, called)
				}
			}
			res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "approve", Arguments: map[string]any{"tool_name": "Bash", "input": map[string]string{"command": "touch file"}, "tool_use_id": "call"}})
			if err != nil || res.IsError {
				t.Fatal(res, err)
			}
			var answer struct{ Behavior string }
			if err := json.Unmarshal([]byte(res.Content[0].(*sdk.TextContent).Text), &answer); err != nil || answer.Behavior != "deny" {
				t.Fatal(answer, err)
			}
		})
	}
}
