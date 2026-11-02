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
			for _, name := range []string{"run_command", "answer_prompt", "ack_inbox", "jump", "kill", "cleanup", "summarize", "approve"} {
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

// Every tool declares its effect (Canon mcp.md): read tools are read-only,
// and each tool that changes state says whether it can destroy, repeats
// harmlessly and reaches beyond the machine.
func TestToolAnnotationsAreTruthful(t *testing.T) {
	ctx := context.Background()
	a, b := sdk.NewInMemoryTransports()
	ss, err := drigamcp.NewServer(drigamcp.Options{Chat: true}).Connect(ctx, a, nil)
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
	type effect struct{ destructive, idempotent, openWorld bool }
	want := map[string]effect{
		"run_command":   {true, false, true},
		"answer_prompt": {true, false, false},
		"kill":          {true, false, false},
		"cleanup":       {true, false, false},
		"ack_inbox":     {false, true, false},
		"jump":          {false, false, false},
		"summarize":     {false, false, true},
		"approve":       {false, false, false},
	}
	for _, tool := range list.Tools {
		h := tool.Annotations
		if h == nil {
			t.Errorf("%s has no annotations", tool.Name)
			continue
		}
		if h.ReadOnlyHint {
			if _, ok := want[tool.Name]; ok {
				t.Errorf("%s changes state but is annotated read-only", tool.Name)
			}
			continue
		}
		if h.DestructiveHint == nil || h.OpenWorldHint == nil {
			t.Errorf("%s changes state without destructive and open-world hints", tool.Name)
			continue
		}
		if w, ok := want[tool.Name]; ok {
			if got := (effect{*h.DestructiveHint, h.IdempotentHint, *h.OpenWorldHint}); got != w {
				t.Errorf("%s annotations = %+v, want %+v", tool.Name, got, w)
			}
			delete(want, tool.Name)
		}
	}
	if len(want) > 0 {
		t.Errorf("tools not served with Chat: %v", want)
	}
}
