package mcp_test

import (
	"context"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"testing"
)

func TestCollectionLimitsAndEnums(t *testing.T) {
	fixture(t)
	cs := connect(t, globals())
	r, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "list_agents", Arguments: map[string]any{"limit": 1}})
	if err != nil || r.IsError {
		t.Fatalf("%+v %v", r, err)
	}
	e := r.StructuredContent.(map[string]any)
	rows := e["data"].(map[string]any)["rows"].([]any)
	if len(rows) != 1 || e["truncated"] != true {
		t.Fatal(e)
	}
	for _, args := range []map[string]any{{"limit": -1}, {"limit": 501}, {"limit": 1.5}} {
		r, err = cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "list_agents", Arguments: args})
		if err == nil && !r.IsError {
			t.Fatalf("invalid limit accepted: %v", args)
		}
	}
	r, err = cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "list_cleanup_candidates", Arguments: map[string]any{"kind": "invalid"}})
	if err == nil && !r.IsError {
		t.Fatal("invalid cleanup kind accepted")
	}
	listing, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listing.Tools {
		schema := tool.InputSchema.(map[string]any)
		props := schema["properties"].(map[string]any)
		if props["limit"] == nil {
			t.Fatal("missing limit schema", tool.Name)
		}
		if tool.Name == "list_cleanup_candidates" && props["kind"].(map[string]any)["enum"] == nil {
			t.Fatal("missing cleanup enum")
		}
	}
}

func TestFlowLimitBoundsNestedArrays(t *testing.T) {
	_, id := fixture(t)
	cs := connect(t, globals())
	r, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "get_flow", Arguments: map[string]any{"id": id, "limit": 1}})
	if err != nil || r.IsError {
		t.Fatal(r, err)
	}
	e := r.StructuredContent.(map[string]any)
	if e["truncated"] != true {
		t.Fatal(e)
	}
}
