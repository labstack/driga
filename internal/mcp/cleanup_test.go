package mcp_test

import (
	"context"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"strings"
	"testing"
)

func TestMCPCleanupReadOnly(t *testing.T) {
	fixture(t)
	cs := connect(t, globals())
	r, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range r.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatal("mutating tool", tool.Name)
		}
		if strings.Contains(tool.Name, "cleanup") {
			if tool.Name != "driga_cleanup_candidates" {
				t.Fatal("mutating cleanup tool", tool.Name)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("cleanup read tool missing")
	}
	result, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "driga_cleanup_candidates", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatal(result, err)
	}
	if result.StructuredContent.(map[string]any)["schema"] != float64(1) {
		t.Fatal(result)
	}
}
