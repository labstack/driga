package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/command"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
)

func registerActions(s *sdk.Server, o Options) {
	for _, tool := range []struct {
		name, description string
		properties        map[string]any
	}{
		{"run_command", "Run one local bounded command through broker preparation and approval. Use literal argv or an explicit shell script; 120 second default, 600 maximum. Process and destructive commands always need a person. Commands run with HOME and TMPDIR in a private scratch directory, so tools that default to the home directory (caches, config) act on that scratch copy, and with PATH=/usr/bin:/bin:/usr/sbin:/sbin, so give the full path of other programs, such as /opt/homebrew/bin/npm. Writes outside the project and scratch are refused.", map[string]any{"argv": map[string]any{"type": "array", "items": map[string]string{"type": "string"}, "minItems": 1}, "script": map[string]any{"type": "string", "minLength": 1}, "cwd": map[string]string{"type": "string"}, "timeout_seconds": map[string]any{"type": "integer", "minimum": 0, "maximum": 600}, "network": map[string]string{"type": "boolean"}}},
		{"answer_prompt", "Answer one pending local Claude permission prompt. Always requires a person in every mode.", map[string]any{"id": map[string]string{"type": "string"}, "allow": map[string]string{"type": "boolean"}}},
		{"ack_inbox", "Acknowledge a local inbox id or all local items.", map[string]any{"id": map[string]string{"type": "string"}, "all": map[string]string{"type": "boolean"}}},
		{"jump", "Jump to a local session terminal. Never types into the terminal.", map[string]any{"session_id": map[string]string{"type": "integer"}}},
		{"kill", "Kill a local agent session or orphan id after identity and idle verification. Force is refused here; use run_command for a second card over surviving identities.", map[string]any{"session_id": map[string]string{"type": "integer"}, "id": map[string]string{"type": "string"}, "force": map[string]string{"type": "boolean"}}},
		{"cleanup", "Clean one local candidate after re-verification.", map[string]any{"id": map[string]string{"type": "string"}}},
		{"summarize", "Refresh one local project summary.", map[string]any{"project": map[string]string{"type": "string"}}},
	} {
		name := tool.name
		schema := map[string]any{"type": "object", "properties": tool.properties, "additionalProperties": false}
		if name == "run_command" {
			schema["oneOf"] = []any{map[string]any{"required": []string{"argv"}, "not": map[string]any{"required": []string{"script"}}}, map[string]any{"required": []string{"script"}, "not": map[string]any{"required": []string{"argv"}}}}
		}
		if name == "answer_prompt" {
			schema["required"] = []string{"id", "allow"}
		}
		s.AddTool(&sdk.Tool{Name: name, Description: tool.description, InputSchema: schema}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			if name == "run_command" {
				var in command.Request
				d := json.NewDecoder(bytes.NewReader(req.Params.Arguments))
				d.DisallowUnknownFields()
				if err := d.Decode(&in); err != nil {
					return nil, err
				}
				if d.Decode(new(any)) != io.EOF {
					return nil, fmt.Errorf("invalid command input")
				}
				if _, err := command.Parse(in); err != nil {
					return nil, err
				}
			}
			if name == "answer_prompt" {
				if _, _, err := chat.ParsePromptAnswer(req.Params.Arguments); err != nil {
					return nil, err
				}
			}
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
