package view

import (
	"context"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"testing"
	"time"
)

func TestAttentionUnboundCompletionsRemainDistinct(t *testing.T) {
	at := time.Now()
	_, review := AttentionAt([]model.InboxItem{{ID: "a", Kind: "finished", TS: at}, {ID: "b", Kind: "finished", TS: at}}, at)
	if review != 2 {
		t.Fatal(review)
	}
}
func TestHistoryStuckExpiresAndPermissionIsCountedOnce(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC().Truncate(time.Minute)
	var id int64
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.UpsertMachine(model.Machine{Hostname: "local"}); err != nil {
			return err
		}
		var err error
		id, err = tx.UpsertSession(model.Session{Label: "waiting", SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "one"}})
		if err != nil {
			return err
		}
		if err := tx.UpsertInboxItem(model.InboxItem{ID: "wait", SessionID: id, Kind: "waiting", TS: now.Add(-30 * time.Minute)}); err != nil {
			return err
		}
		return tx.RecordPanelPolls(now.Add(-5 * time.Minute))
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.StartPromptHistory(context.Background(), "p", id, now.Add(-10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	h, err := PanelHistory(st, Filter{}, now)
	if err != nil {
		t.Fatal(err)
	}
	last := h.Attention[len(h.Attention)-1]
	if last.Waiting != 1 || last.Stuck != nil {
		t.Fatalf("%+v", last)
	}
}
