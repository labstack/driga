package feed

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestNeedsYouFleetPolicyAndEnd(t *testing.T) {
	at := time.Unix(100, 0).UTC()
	i := model.InboxItem{ID: "wait", Kind: "waiting", Reason: "permission", Tool: "Bash", Detail: "run ghp_secret command", Summary: "run ghp_secret command", Source: "both", WaitingSinceAt: at, TS: at, EndedAt: &at, Jump: &model.InboxJump{Kind: "resume", Command: "claude --resume 's'"}}
	for _, policy := range []ShareText{None, Titles, Snippets} {
		_, b := Encode(Inbox, i, policy)
		var got model.InboxItem
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		if got.Reason != "permission" || got.Source != "both" || got.EndedAt == nil || got.AckedAt != nil || got.Jump == nil {
			t.Fatalf("lost contract: %s", b)
		}
		if policy == None && got.Tool != "" {
			t.Fatal("none exposed tool")
		}
		if policy != Snippets && got.Detail != "" {
			t.Fatal("detail leak")
		}
		if strings.Contains(string(b), "ghp_secret") {
			t.Fatal("unscrubbed detail")
		}
	}
}
