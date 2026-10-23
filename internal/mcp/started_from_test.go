package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/labstack/driga/internal/store"
)

func TestStartedFromMCPAndCLIJSON(t *testing.T) {
	st, id := fixture(t)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		s, err := tx.Session(id)
		if err != nil {
			return err
		}
		s.StartedFrom = "Fixture Editor"
		_, err = tx.UpsertSession(s)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	cs := connect(t, globals())
	for _, tc := range []toolCase{
		{name: "list_agents", args: map[string]any{"machine": "local"}, cli: []string{"agents", "--machine", "local"}},
		{name: "get_agent", args: map[string]any{"id": id}, cli: []string{"agents", "show", fmt.Sprint(id)}},
		{name: "get_flow", args: map[string]any{"id": id}, cli: []string{"flow", fmt.Sprint(id)}},
	} {
		r := call(t, cs, tc)
		b, err := json.Marshal(r.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		for _, payload := range [][]byte{b, cliJSON(t, tc.cli)} {
			if !bytes.Contains(payload, []byte(`"started_from":"Fixture Editor"`)) || !bytes.Contains(payload, []byte(`"schema":2`)) {
				t.Fatalf("%s: %s", tc.name, payload)
			}
		}
	}
}
