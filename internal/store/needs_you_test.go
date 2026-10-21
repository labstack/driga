package store

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestNeedsYouStoreMergeEndAndOrder(t *testing.T) {
	st := openTest(t, "", Memory)
	at := time.Unix(100, 0).UTC()
	wait := func(id, reason, source string, ts time.Time) model.InboxItem {
		var i model.InboxItem
		b, _ := json.Marshal(map[string]any{"id": id, "session_id": int64(1), "kind": "waiting", "reason": reason, "source": source, "waiting_since_at": ts, "occurred_at": ts, "detail": "detail", "summary": "waiting for input"})
		json.Unmarshal(b, &i)
		return i
	}
	mustTx(t, st, func(tx *Tx) error {
		if err := tx.UpsertInboxItem(wait("first", "permission", "hook", at)); err != nil {
			return err
		}
		if err := tx.UpsertInboxItem(wait("second", "question", "hook", at.Add(time.Second))); err != nil {
			return err
		}
		return tx.UpsertInboxItem(model.InboxItem{ID: "done", Kind: "finished", TS: at.Add(time.Hour)})
	})
	rows, err := st.Inbox(time.Time{}, false)
	if err != nil || len(rows) != 2 || rows[0].ID != "first" {
		t.Fatalf("one wait, oldest first: %+v %v", rows, err)
	}
	b, _ := json.Marshal(rows[0])
	var v map[string]any
	json.Unmarshal(b, &v)
	if v["reason"] != "question" {
		t.Fatalf("reason not updated: %s", b)
	}
	mustTx(t, st, func(tx *Tx) error { return tx.CloseAttentionBefore(1, at.Add(2*time.Second)) })
	history, err := st.Inbox(time.Time{}, true)
	if err != nil || len(history) != 2 {
		t.Fatal(history, err)
	}
	for _, i := range history {
		if i.Kind != "waiting" {
			continue
		}
		b, _ := json.Marshal(i)
		json.Unmarshal(b, &v)
		if i.AckedAt != nil || v["ended_at"] == nil {
			t.Fatalf("end became ack: %s", b)
		}
	}
	rows, err = st.Inbox(time.Time{}, false)
	if err != nil || len(rows) != 1 || rows[0].ID != "done" {
		t.Fatal(rows, err)
	}
}

func TestNeedsYouEndedIsNotRemoteAck(t *testing.T) {
	st := openTest(t, "", Memory)
	at := time.Unix(100, 0).UTC()
	mustTx(t, st, func(tx *Tx) error {
		return tx.ForMachine("paired").ApplyInbox(model.InboxItem{ID: "w", SessionID: 1, Kind: "waiting", TS: at, WaitingSinceAt: at, EndedAt: &at})
	})
	got, err := st.PendingAcks(Scope{})
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	mustTx(t, st, func(tx *Tx) error { return tx.ForMachine("paired").MarkAckPending("w", at) })
	got, err = st.PendingAcks(Scope{})
	if err != nil || len(got) != 1 {
		t.Fatalf("explicit ack lost after automatic end: %+v %v", got, err)
	}
}

func TestNeedsYouUnknownHookRetainsCLIReason(t *testing.T) {
	st := openTest(t, "", Memory)
	at := time.Unix(100, 0).UTC()
	mustTx(t, st, func(tx *Tx) error {
		if err := tx.UpsertInboxItem(model.InboxItem{ID: "cli", SessionID: 1, Kind: "waiting", TS: at, Reason: "permission", Source: "agents_cli"}); err != nil {
			return err
		}
		return tx.UpsertInboxItem(model.InboxItem{ID: "hook", SessionID: 1, Kind: "waiting", TS: at.Add(time.Second), Reason: "unknown", Source: "hook"})
	})
	rows, _ := st.Inbox(time.Time{}, false)
	if len(rows) != 1 || rows[0].Reason != "permission" || rows[0].Source != "both" {
		t.Fatal(rows)
	}
}

func TestNeedsYouCLIUpdatesWhenHookHasNoReason(t *testing.T) {
	st := openTest(t, "", Memory)
	at := time.Unix(100, 0).UTC()
	mustTx(t, st, func(tx *Tx) error {
		for n, i := range []model.InboxItem{{ID: "h", Reason: "unknown", Source: "hook"}, {ID: "c1", Reason: "permission", Source: "agents_cli"}, {ID: "c2", Reason: "question", Source: "agents_cli"}} {
			i.Kind = "waiting"
			i.SessionID = 1
			i.TS = at.Add(time.Duration(n) * time.Second)
			if err := tx.UpsertInboxItem(i); err != nil {
				return err
			}
		}
		return nil
	})
	rows, _ := st.Inbox(time.Time{}, false)
	if len(rows) != 1 || rows[0].Reason != "question" || rows[0].Source != "both" {
		t.Fatal(rows)
	}
}

func TestNeedsYouPrunesAlertAndAliasMetadata(t *testing.T) {
	st := openTest(t, "", Memory)
	now := time.Now().UTC()
	old := now.Add(-31 * 24 * time.Hour)
	mustTx(t, st, func(tx *Tx) error {
		for _, key := range []string{"inbox_alert:old", "inbox_alert_session:old"} {
			if err := tx.SetMeta(key, old.Format(time.RFC3339Nano)); err != nil {
				return err
			}
		}
		if err := tx.SetMeta("inbox_alias:old", "expired"); err != nil {
			return err
		}
		if err := tx.SetMeta("inbox_alert:current", now.Format(time.RFC3339Nano)); err != nil {
			return err
		}
		return tx.Prune(now)
	})
	for _, key := range []string{"inbox_alert:old", "inbox_alert_session:old", "inbox_alias:old"} {
		if _, err := st.Meta(key); !errors.Is(err, ErrNotFound) {
			t.Fatal("unbounded metadata", key, err)
		}
	}
	if _, err := st.Meta("inbox_alert:current"); err != nil {
		t.Fatal(err)
	}
}
