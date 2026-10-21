package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestNeedsYouCLIAndMCPContract(t *testing.T) {
	st, id := fixture(t)
	at := time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.UpsertInboxItem(model.InboxItem{ID: "needs-new", SessionID: id, Kind: "waiting", Reason: "question", Tool: "AskUserQuestion", Detail: "private pending command", Summary: "private pending command", TS: at, WaitingSinceAt: at, Source: "hook"}); err != nil {
			return err
		}
		sid, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "old-wait"}})
		if err != nil {
			return err
		}
		return tx.UpsertInboxItem(model.InboxItem{ID: "needs-old", SessionID: sid, Kind: "waiting", Reason: "permission", Tool: "Bash", Detail: "private pending command", TS: at.Add(-time.Hour), WaitingSinceAt: at.Add(-time.Hour), Source: "agents_cli"})
	}); err != nil {
		t.Fatal(err)
	}
	cli := cliJSON(t, []string{"inbox", "list"})
	cs := connect(t, globals())
	result := call(t, cs, toolCase{name: "list_inbox"})
	for _, raw := range [][]byte{cli, canonical(t, result.StructuredContent)} {
		if strings.Contains(string(raw), "private pending command") {
			t.Fatalf("titles leaked command: %s", raw)
		}
		var v struct {
			Schema int
			Data   struct {
				Rows []struct {
					ID, Reason, Tool, Source string
					WaitedMS                 int64     `json:"waited_ms"`
					WaitingSinceAt           time.Time `json:"waiting_since_at"`
				}
			}
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		if v.Schema != 2 || len(v.Data.Rows) < 2 || v.Data.Rows[0].ID != "needs-old" || v.Data.Rows[1].ID != "needs-new" || v.Data.Rows[0].Reason != "permission" || v.Data.Rows[0].Tool != "Bash" || v.Data.Rows[0].WaitingSinceAt.IsZero() {
			t.Fatalf("contract/order: %s", raw)
		}
	}
}
