package store

import (
	"reflect"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestActiveInboxIdentitiesMachineLifecycleAndPayloadIsolation(t *testing.T) {
	st := openTest(t, "", Memory)
	now := time.Unix(100, 0)
	mustTx(t, st, func(tx *Tx) error {
		for _, item := range []model.InboxItem{
			{ID: "active", SessionID: 1, Kind: "finished", TS: now},
			{ID: "waiting", SessionID: 2, Kind: "waiting", TS: now, AckPendingAt: &now},
			{ID: "acked", SessionID: 1, Kind: "finished", TS: now, AckedAt: &now},
			{ID: "ended", SessionID: 1, Kind: "finished", TS: now, EndedAt: &now},
			{ID: "old", SessionID: 1, Kind: "finished", TS: time.Unix(-1, 0)},
		} {
			if err := tx.UpsertInboxItem(item); err != nil {
				return err
			}
		}
		if err := tx.ForMachine("remote").UpsertInboxItem(model.InboxItem{ID: "remote", SessionID: 3, Kind: "finished", TS: now}); err != nil {
			return err
		}
		// A projection of identities must not parse unrelated persisted JSON.
		return tx.exec("UPDATE inbox_items SET jump='invalid unused JSON' WHERE machine_id=? AND id='active'", tx.machineID)
	})
	got, err := st.ActiveInboxIdentities()
	want := []InboxIdentity{{ID: "active", SessionID: 1}, {ID: "waiting", SessionID: 2}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("identities: %+v, %v", got, err)
	}
}
