package collector

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestWriteInboxJumpsPreservesScopeAndClearsMissingDestination(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx, now := context.Background(), time.Unix(100, 0)
	old := &model.InboxJump{Kind: "command", Command: "old"}
	next := &model.InboxJump{Kind: "command", Command: "new"}
	local := model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "local"}}
	remote := model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "remote"}}
	err = st.Tx(ctx, func(tx *store.Tx) error {
		var err error
		local.ID, err = tx.UpsertSession(local)
		if err != nil {
			return err
		}
		remote.ID, err = tx.ForMachine("remote").UpsertSession(remote)
		if err != nil {
			return err
		}
		for _, item := range []model.InboxItem{
			{ID: "active", Kind: "finished", TS: now},
			{ID: "acked", Kind: "finished", TS: now, AckedAt: &now},
			{ID: "ended", Kind: "finished", TS: now, EndedAt: &now},
		} {
			item.SessionID, item.Jump = local.ID, old
			if err := tx.UpsertInboxItem(item); err != nil {
				return err
			}
		}
		return tx.ForMachine("remote").UpsertInboxItem(model.InboxItem{ID: "active", SessionID: remote.ID, Kind: "finished", TS: now, Jump: old})
	})
	if err != nil {
		t.Fatal(err)
	}
	ss := []model.Session{local, remote}
	for _, jump := range []*model.InboxJump{next, nil} {
		if err := st.Tx(ctx, func(tx *store.Tx) error {
			return writeInboxJumps(tx, ss, map[string]*model.InboxJump{sessionKey("claude", "local"): jump, sessionKey("claude", "remote"): next})
		}); err != nil {
			t.Fatal(err)
		}
		items, err := st.InboxScoped(time.Time{}, true, store.Scope{})
		if err != nil || len(items) != 4 {
			t.Fatalf("inbox: %+v %v", items, err)
		}
		for _, item := range items {
			want := old
			if item.MachineID == st.MachineID() && item.ID == "active" {
				want = jump
			}
			if !reflect.DeepEqual(item.Jump, want) {
				t.Fatalf("destination changed outside active local scope or failed nil clear: %+v want %+v", item, want)
			}
		}
	}
}
