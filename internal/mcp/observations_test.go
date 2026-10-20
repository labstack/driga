package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/labstack/driga/internal/store"
)

func TestFlowObservedCLIAndMCP(t *testing.T) {
	st, id := fixture(t)
	at := time.Now().UTC().Truncate(time.Millisecond)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.Observe(id, at, 12288, 250) }); err != nil {
		t.Fatal(err)
	}
	check := func(raw []byte) map[string]any {
		t.Helper()
		var envelope map[string]any
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatal(err)
		}
		o := envelope["data"].(map[string]any)["lead"].(map[string]any)["observed"].(map[string]any)
		if len(o["log_bytes_per_min"].([]any)) != 60 || len(o["cpu_ms_per_min"].([]any)) != 60 || o["last_growth_bytes"] != float64(12288) || o["last_growth_at"] != at.Format(time.RFC3339Nano) {
			t.Fatalf("observed=%+v", o)
		}
		return o
	}
	want := check(cliJSON(t, []string{"flow", fmt.Sprint(id)}))
	for _, limit := range []int{1, 100, 500} {
		result := call(t, connect(t, globals()), toolCase{name: "get_flow", args: map[string]any{"id": id, "limit": limit}})
		raw, _ := json.Marshal(result.StructuredContent)
		got := check(raw)
		a, _ := json.Marshal(want)
		b, _ := json.Marshal(got)
		if string(a) != string(b) {
			t.Fatalf("CLI/MCP differ: %s / %s", a, b)
		}
	}
}
