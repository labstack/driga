package mcp_test

import (
	"context"
	"encoding/json"
	drigamcp "github.com/labstack/driga/internal/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"testing"
)

func TestRunCommandMCPRegistration(t *testing.T) {
	for _, actions := range []bool{false, true} {
		t.Run(map[bool]string{false: "external-or-ask", true: "chat"}[actions], func(t *testing.T) {
			ctx := context.Background()
			called := 0
			o := drigamcp.Options{Chat: actions, Action: func(_ context.Context, name string, raw json.RawMessage) (string, error) {
				if name != "run_command" {
					t.Fatal(name)
				}
				called++
				return "broker result", nil
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
			list, err := cs.ListTools(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			found := 0
			for _, tool := range list.Tools {
				if tool.Name == "run_command" {
					found++
				}
			}
			if (found == 1) != actions {
				t.Fatal("run_command registration", found, actions)
			}
			if !actions {
				return
			}
			for _, args := range []map[string]any{{"argv": []string{"echo", "literal; text"}, "cwd": ".", "timeout_seconds": 120, "network": false}, {"script": "echo script", "timeout_seconds": 600, "network": true}} {
				r, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "run_command", Arguments: args})
				if err != nil || r.IsError {
					t.Fatal(r, err)
				}
			}
			for _, args := range []map[string]any{{}, {"argv": []string{"true"}, "script": "true"}, {"argv": []string{}}, {"argv": []string{"true"}, "timeout_seconds": 601}, {"argv": []string{"true"}, "grant": true}, {"argv": []string{"true"}, "targets": []int{1}}} {
				r, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "run_command", Arguments: args})
				if err == nil && !r.IsError {
					t.Fatal("invalid request accepted", args)
				}
			}
			if called != 2 {
				t.Fatal("invalid request forwarded", called)
			}
		})
	}
}
