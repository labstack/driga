package collector

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestAlertInboxIgnoresUnrelatedSessions(t *testing.T) {
	d, st, _, _ := setup(t)
	n := &fakeNotifier{}
	d.Notifier = n
	d.Cfg.Inbox.Alerts = "all"
	ended := d.now().Add(-time.Minute)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		id, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "needed", Title: "Ended session"}, EndedAt: &ended})
		if err != nil {
			return err
		}
		return tx.UpsertInboxItem(model.InboxItem{ID: "done", SessionID: id, Kind: "finished", TS: d.now()})
	}); err != nil {
		t.Fatal(err)
	}
	if err := alertInbox(context.Background(), d, st); err != nil {
		t.Fatal(err)
	}
	if len(n.notes) != 1 || n.notes[0].Body != "Ended session · turn finished" {
		t.Fatalf("ended session still needs its notification label: %+v", n.notes)
	}
	measure := func() float64 {
		return testing.AllocsPerRun(3, func() {
			if err := alertInbox(context.Background(), d, st); err != nil {
				t.Fatal(err)
			}
		})
	}
	before := measure()
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		for i := range 100 {
			s := model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: fmt.Sprint(i), Title: "Unrelated history"}}
			if i%2 == 0 {
				s.EndedAt = &ended
			}
			if _, err := tx.UpsertSession(s); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	after := measure()
	t.Logf("alert allocations: before=%.0f; with 100 unrelated live/ended sessions=%.0f", before, after)
	if after > before+100 {
		t.Fatalf("unrelated sessions added %.0f alert allocations", after-before)
	}
	if len(n.notes) != 1 {
		t.Fatal("already alerted item notified again")
	}
}

func TestAlertInboxAllocationsFlatWithUnackedBacklog(t *testing.T) {
	d, st, _, _ := setup(t)
	d.Notifier = &fakeNotifier{}
	d.Cfg.Inbox.Alerts = "all"
	d.Cfg.Inbox.AlertAfter = time.Hour
	// Keep one delayed candidate throughout: label and metadata reads must also
	// stay flat, not just the empty-candidate fast path.
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		id, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "pending", Title: "Pending"}})
		if err != nil {
			return err
		}
		return tx.UpsertInboxItem(model.InboxItem{ID: "pending", SessionID: id, Kind: "waiting", TS: d.now()})
	}); err != nil {
		t.Fatal(err)
	}
	measure := func() float64 {
		return testing.AllocsPerRun(5, func() {
			if err := alertInbox(context.Background(), d, st); err != nil {
				t.Fatal(err)
			}
		})
	}
	baseline := measure()
	for _, count := range []int{100, 1000} {
		if err := st.Tx(context.Background(), func(tx *store.Tx) error {
			for i := range count {
				id, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: fmt.Sprint(i), Title: "Backlog"}})
				if err != nil {
					return err
				}
				if err := tx.UpsertInboxItem(model.InboxItem{ID: fmt.Sprint(i), SessionID: id, Kind: "finished", TS: d.now()}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		// Consume each newly committed batch; the items remain unacknowledged.
		if err := alertInbox(context.Background(), d, st); err != nil {
			t.Fatal(err)
		}
		got := measure()
		t.Logf("baseline %.0f allocations; %d unacked items: %.0f", baseline, count, got)
		if got > baseline+100 {
			t.Fatalf("%d backlog items added %.0f steady-state allocations", count, got-baseline)
		}
	}
}
