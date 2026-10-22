package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/store"
)

func TestMCPEndedLabel(t *testing.T) {
	st, id := fixture(t)
	s, err := st.Session(id)
	if err != nil {
		t.Fatal(err)
	}
	s.Label, s.EndedAt = "ended", &s.LastActivityAt
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { _, err := tx.UpsertSession(s); return err }); err != nil {
		t.Fatal(err)
	}
	cs := connect(t, globals())
	for _, tc := range []toolCase{{name: "get_agent", args: map[string]any{"id": id, "machine": "local"}}, {name: "list_agents", args: map[string]any{"all": true, "machine": "local"}}} {
		got := call(t, cs, tc)
		b, err := json.Marshal(got.StructuredContent)
		if err != nil || !strings.Contains(string(b), `"label":"ended"`) || !strings.Contains(string(b), `"schema":2`) {
			t.Fatalf("%s: %s %v", tc.name, b, err)
		}
	}
}
