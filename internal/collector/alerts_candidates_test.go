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

func TestAlertCandidatesMachineWarningDoesNotNotifyThreeTimes(t *testing.T) {
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
	for _, offset := range []time.Duration{0, time.Minute, 2 * time.Minute} {
		// Three collector cycles, including a restart and refreshed readings.
		d.State = NewState()
		if err := st.Tx(context.Background(), func(tx *store.Tx) error {
			return tx.RefreshMachineWarning("low_disk", "Disk still low", nil)
		}); err != nil {
			t.Fatal(err)
		}
		d.Now = func() time.Time { return at.Add(offset) }
		if err := alertInbox(context.Background(), d, st); err != nil {
			t.Fatal(err)
		}
	}
	if len(n.notes) != 1 {
		t.Fatal(n.notes)
	}
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { _, err := tx.AckInbox("health", at.Add(time.Minute)); return err }); err != nil {
		t.Fatal(err)
	}
	d.Now = func() time.Time { return at.Add(2 * time.Minute) }
	if err := alertInbox(context.Background(), d, st); err != nil {
		t.Fatal(err)
	}
	if len(n.notes) != 1 {
		t.Fatal(n.notes)
	}
}

func TestMachineNotificationsRearmOnlyAfterRecovery(t *testing.T) {
	d, st, _, _ := setup(t)
	n := &fakeNotifier{}
	d.Notifier = n
	d.Cfg.Machine.DiskFreeMin = 20_000_000_000
	at := d.now()
	step := func(offset time.Duration, free uint64, pressure string, prune bool, want int) {
		t.Helper()
		now := at.Add(offset)
		d.Now = func() time.Time { return now }
		d.State = NewState()
		if err := st.Tx(t.Context(), func(tx *store.Tx) error {
			if prune {
				if err := tx.Prune(now); err != nil {
					return err
				}
			}
			sample := model.MachineSample{TS: now, DiskTotal: 1_000_000_000_000, DiskFree: free, PressureLevel: pressure}
			if err := lowDiskInbox(tx, d.Cfg.Machine, sample, now); err != nil {
				return err
			}
			return memoryPressureInbox(tx, sample, now)
		}); err != nil {
			t.Fatal(err)
		}
		if err := alertInbox(t.Context(), d, st); err != nil {
			t.Fatal(err)
		}
		if len(n.notes) != want {
			t.Fatalf("at %s: %+v", offset, n.notes)
		}
	}
	step(0, 12_154_470_400, "critical", false, 1)
	if n.notes[0].Title != "Low disk space" || n.notes[0].Body != "12.2 GB free, below 20 GB" {
		t.Fatal(n.notes)
	}
	step(time.Minute, 12_000_000_000, "critical", false, 2)
	step(2*time.Minute, 11_000_000_000, "", false, 2) // Unknown is not recovery.
	step(3*time.Minute, 11_000_000_000, "critical", false, 2)
	step(4*time.Minute, 11_000_000_000, "critical", false, 2)
	step(31*24*time.Hour, 11_000_000_000, "critical", true, 2)
	step(31*24*time.Hour+time.Minute, 30_000_000_000, "normal", false, 2)
	// Recovered conditions re-arm even inside the session repeat interval.
	step(31*24*time.Hour+2*time.Minute, 12_000_000_000, "critical", false, 3)
	step(31*24*time.Hour+3*time.Minute, 12_000_000_000, "critical", false, 4)
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
