package mcp

import (
	"context"
	"fmt"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerActions(s *sdk.Server, o Options) {
	for _, tool := range []struct {
		name, description string
		properties        map[string]any
	}{
		{"ack_inbox", "Acknowledge a local inbox id or all local items.", map[string]any{"id": map[string]string{"type": "string"}, "all": map[string]string{"type": "boolean"}}},
		{"jump", "Jump to a local session terminal. Never types into the terminal.", map[string]any{"session_id": map[string]string{"type": "integer"}}},
		{"kill", "Kill a local agent session or orphan id after identity and idle verification. Force also escalates survivors.", map[string]any{"session_id": map[string]string{"type": "integer"}, "id": map[string]string{"type": "string"}, "force": map[string]string{"type": "boolean"}}},
		{"cleanup", "Clean one local candidate after re-verification.", map[string]any{"id": map[string]string{"type": "string"}}},
		{"summarize", "Refresh one local project summary.", map[string]any{"project": map[string]string{"type": "string"}}},
	} {
		name := tool.name
		s.AddTool(&sdk.Tool{Name: name, Description: tool.description, InputSchema: map[string]any{"type": "object", "properties": tool.properties, "additionalProperties": false}}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			if o.Action == nil {
				return nil, fmt.Errorf("Chat actions unavailable")
			}
			text, err := o.Action(ctx, name, req.Params.Arguments)
			if err != nil {
				return nil, err
			}
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}}, nil
		})
	}
	s.AddTool(&sdk.Tool{Name: "approve", Description: "Request driga permission for a tool. Cannot decide cards or change trust mode.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"tool_name": map[string]string{"type": "string"}, "input": map[string]string{"type": "object"}, "tool_use_id": map[string]string{"type": "string"}}, "required": []string{"tool_name", "input", "tool_use_id"}, "additionalProperties": false}}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		if o.Approve == nil {
			return nil, fmt.Errorf("Chat approval unavailable")
		}
		raw, err := o.Approve(ctx, req.Params.Arguments)
		if err != nil {
			return nil, err
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(raw)}}}, nil
	})
}
