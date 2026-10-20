package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"testing"
	"time"
)

func TestFlowMapContractCLIParity(t *testing.T) {
	st, id := fixture(t)
	now := time.Now().UTC().Truncate(time.Minute)
	ack := now.Add(-time.Minute)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.InsertEvent(model.Event{Adapter: "claude", ExternalID: "session", Kind: "otlp:tool_result", TS: now.Add(-2 * time.Minute)}); err != nil {
			return err
		}
		if err := tx.Observe(id, now.Add(-3*time.Minute), 400, 25); err != nil {
			return err
		}
		return tx.UpsertInboxItem(model.InboxItem{ID: "acked-flow", SessionID: id, Kind: "waiting", TS: now.Add(-4 * time.Minute), AckedAt: &ack})
	}); err != nil {
		t.Fatal(err)
	}
	cs := connect(t, globals())
	r := call(t, cs, toolCase{name: "get_flow", args: map[string]any{"id": id, "minutes": 240, "limit": 500}})
	got := canonical(t, r.StructuredContent)
	want := canonical(t, json.RawMessage(cliJSON(t, []string{"flow", fmt.Sprint(id), "--minutes", "240"})))
	if !bytes.Equal(got, want) {
		t.Fatalf("MCP/CLI mismatch\n%s\n%s", got, want)
	}
	data := r.StructuredContent.(map[string]any)["data"].(map[string]any)
	lead := data["lead"].(map[string]any)
	for _, key := range []string{"started_at", "ended_at", "parent_id", "mixer", "events", "waits", "observed", "retained_from_at"} {
		if _, ok := lead[key]; !ok {
			t.Error("missing node field", key)
		}
	}
	for _, key := range []string{"gaps", "attention", "window_minutes"} {
		if _, ok := data[key]; !ok {
			t.Error("missing flow field", key)
		}
	}
	listing, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listing.Tools {
		if tool.Name == "get_flow" {
			p := tool.InputSchema.(map[string]any)["properties"].(map[string]any)
			if len(p["minutes"].(map[string]any)["enum"].([]any)) != 3 {
				t.Fatal("missing zoom schema")
			}
		}
	}
	bad, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "get_flow", Arguments: map[string]any{"id": id, "minutes": 30}})
	if err == nil && !bad.IsError {
		t.Fatal("invalid zoom accepted")
	}
}
