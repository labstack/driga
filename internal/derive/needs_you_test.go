package derive

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestNeedsYouNotificationReasonsAndIdle(t *testing.T) {
	for _, tc := range []struct {
		typ, reason string
		count       int
	}{
		{"permission_prompt", "permission", 1}, {"agent_needs_input", "question", 1},
		{"elicitation_dialog", "elicitation", 1}, {"idle_prompt", "", 0}, {"", "unknown", 1}, {"future_type", "unknown", 1},
	} {
		t.Run(tc.typ, func(t *testing.T) {
			var h model.HookEvent
			b, _ := json.Marshal(map[string]any{"adapter": "claude", "session_id": "s", "event": "Notification", "notification_type": tc.typ, "occurred_at": time.Unix(100, 0).UTC()})
			if err := json.Unmarshal(b, &h); err != nil {
				t.Fatal(err)
			}
			got := InboxItems([]model.Session{{ID: 1, SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "s"}}}, nil, []model.HookEvent{h}, time.Time{})
			if len(got) != tc.count {
				t.Fatalf("%s: %+v", tc.typ, got)
			}
			if tc.count > 0 {
				b, _ := json.Marshal(got[0])
				var row map[string]any
				json.Unmarshal(b, &row)
				if row["reason"] != tc.reason || row["source"] != "hook" || row["waiting_since_at"] == nil {
					t.Fatalf("missing wait metadata: %s", b)
				}
			}
		})
	}
}

func TestNeedsYouWorkerRetainsIdentity(t *testing.T) {
	s := []model.Session{{ID: 1, SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "parent"}}, {ID: 2, SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "worker"}}}
	got := InboxItems(s, map[int64]int64{2: 1}, []model.HookEvent{{Adapter: "claude", SessionID: "worker", Event: "Notification", TS: time.Unix(100, 0)}}, time.Time{})
	if len(got) != 1 || got[0].SessionID != 2 {
		t.Fatalf("worker wait attributed to parent: %+v", got)
	}
}

func TestNeedsYouFirstCollectionDoesNotAcknowledgeWait(t *testing.T) {
	at := time.Unix(100, 0).UTC()
	got := InboxItems([]model.Session{{ID: 1, SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "s"}}}, nil, []model.HookEvent{{Adapter: "claude", SessionID: "s", Event: "Notification", NotificationType: "permission_prompt", TS: at}}, at.Add(time.Second))
	if len(got) != 1 || got[0].AckedAt != nil || got[0].EndedAt != nil {
		t.Fatalf("collection manufactured a user acknowledgement: %+v", got)
	}
}
