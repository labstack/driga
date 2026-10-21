package collector

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestAlertCandidatesDelaySuppressionAndLateArrival(t *testing.T) {
	d, st, _, _ := setup(t)
	n := &fakeNotifier{}
	d.Notifier = n
	d.Cfg.Inbox.Alerts = "all"
	d.Cfg.Inbox.AlertAfter = time.Minute
	d.Cfg.Inbox.AlertRepeat = 5 * time.Minute
	at := d.now()
	put := func(i model.InboxItem) {
		t.Helper()
		if err := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.UpsertInboxItem(i) }); err != nil {
			t.Fatal(err)
		}
	}
	run := func(offset time.Duration, want int) {
		t.Helper()
		d.Now = func() time.Time { return at.Add(offset) }
		if err := alertInbox(context.Background(), d, st); err != nil {
			t.Fatal(err)
		}
		if len(n.notes) != want {
			t.Fatalf("at %s: %+v", offset, n.notes)
		}
	}
	put(model.InboxItem{ID: "delayed", SessionID: 1, Kind: "waiting", TS: at})
	put(model.InboxItem{ID: "answered", SessionID: 2, Kind: "waiting", TS: at})
	run(0, 0)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { _, err := tx.AckInbox("answered", at.Add(30*time.Second)); return err }); err != nil {
		t.Fatal(err)
	}
	run(time.Minute, 1)
	put(model.InboxItem{ID: "suppressed", SessionID: 1, Kind: "finished", TS: at.Add(2 * time.Minute)})
	run(2*time.Minute, 1)
	d.State = NewState() // Restarting cannot resurrect consumed/suppressed items.
	run(10*time.Minute, 1)
	// A timestamp watermark would lose this out-of-order arrival.
	put(model.InboxItem{ID: "late", SessionID: 3, Kind: "finished", TS: at.Add(-time.Hour)})
	run(10*time.Minute, 2)
}

func TestAlertCandidatesMachineWarningsRepeatUntilClosed(t *testing.T) {
	d, st, _, _ := setup(t)
	n := &fakeNotifier{}
	d.Notifier = n
	d.Cfg.Inbox.AlertRepeat = time.Minute
	at := d.now()
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.UpsertInboxItem(model.InboxItem{ID: "health", Kind: "low_disk", Summary: "Disk low", TS: at})
	}); err != nil {
		t.Fatal(err)
	}
	for _, offset := range []time.Duration{0, 30 * time.Second, time.Minute} {
		d.Now = func() time.Time { return at.Add(offset) }
		if err := alertInbox(context.Background(), d, st); err != nil {
			t.Fatal(err)
		}
	}
	if len(n.notes) != 2 {
		t.Fatal(n.notes)
	}
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { _, err := tx.AckInbox("health", at.Add(time.Minute)); return err }); err != nil {
		t.Fatal(err)
	}
	d.Now = func() time.Time { return at.Add(2 * time.Minute) }
	if err := alertInbox(context.Background(), d, st); err != nil {
		t.Fatal(err)
	}
	if len(n.notes) != 2 {
		t.Fatal(n.notes)
	}
}

func TestAlertCandidatesRemoteReplayAndMetadataExpiry(t *testing.T) {
	d, st, _, _ := setup(t)
	n := &fakeNotifier{}
	d.Notifier = n
	at := d.now()
	if err := fleet.SavePeers(filepath.Join(config.DataDir(), "fleet"), []fleet.Peer{{MachineID: "peer", DeviceID: "fixture"}}); err != nil {
		t.Fatal(err)
	}
	put := func(now time.Time, replace bool) {
		t.Helper()
		if err := st.Tx(context.Background(), func(tx *store.Tx) error {
			if replace {
				if err := tx.ClearRemoteRows("peer"); err != nil {
					return err
				}
			}
			r := tx.ForMachine("peer")
			if err := r.UpsertMachine(model.Machine{LastContactAt: now}); err != nil {
				return err
			}
			id, err := r.UpsertSession(model.Session{StartedFrom: "Editor", SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "peer-session", Title: "Peer task"}})
			if err != nil {
				return err
			}
			return r.ApplyInbox(model.InboxItem{ID: "remote", SessionID: id, Kind: "waiting", TS: now.Add(-time.Hour)})
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, replace := range []bool{false, false, true} {
		put(at, replace)
		d.State = NewState()
		if err := alertInbox(context.Background(), d, st); err != nil {
			t.Fatal(err)
		}
		if len(n.notes) != 1 || n.notes[0].ItemID != "" || n.notes[0].Body != "Peer task · waiting for input · from Editor" {
			t.Fatal(n.notes)
		}
	}
	// Retained remote IDs can outlive the source seen metadata. Preserve the
	// existing expiry semantics if that same item is still open after pruning.
	later := at.Add(31 * 24 * time.Hour)
	put(later, false)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.Prune(later) }); err != nil {
		t.Fatal(err)
	}
	d.Now = func() time.Time { return later }
	if err := alertInbox(context.Background(), d, st); err != nil {
		t.Fatal(err)
	}
	if len(n.notes) != 2 {
		t.Fatal(n.notes)
	}
}
