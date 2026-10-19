package collector

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestIdleFeedRateBounded(t *testing.T) {
	d, st, _, _ := setup(t)
	d.Cfg.Fleet.Serve = true
	d.Cfg.StalledAfter = time.Hour
	now := d.Now()
	d.Now = func() time.Time { return now }
	pass(t, d, st, Plan{Procs: true, Machine: true})
	_, _, seq, _ := st.FeedBounds()
	// Adapters and spool run every wake even when the slower sources aren't due.
	for range 600 {
		now = now.Add(time.Second)
		pass(t, d, st, Plan{})
	}
	rows := feedRows(t, st, seq)
	if len(rows) > 20 {
		t.Fatalf("idle feed emitted %d rows in ten minutes; want <=20", len(rows))
	}
	// Actual errors and recovery must bypass the freshness throttle.
	now = now.Add(time.Second)
	d.Adapters[0].(*fakeAdapter).err = errors.New("source unavailable")
	pass(t, d, st, Plan{})
	_, _, failed, _ := st.FeedBounds()
	if failed <= seq+int64(len(rows)) {
		t.Fatal("source error was not published immediately")
	}
	d.Adapters[0].(*fakeAdapter).err = nil
	now = now.Add(time.Second)
	pass(t, d, st, Plan{})
	_, _, recovered, _ := st.FeedBounds()
	if recovered <= failed {
		t.Fatal("source recovery was not published immediately")
	}
}

func TestSnapshotNeverEmptySessionKey(t *testing.T) {
	d, _, _, _ := setup(t)
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	old := now.Add(-31 * 24 * time.Hour)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.UpsertMachine(model.Machine{Hostname: "peer"}); err != nil {
			return err
		}
		id, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "codex", ExternalID: "old"}, EndedAt: &old})
		if err != nil {
			return err
		}
		if err := tx.UpsertInboxItem(model.InboxItem{ID: "recent", SessionID: id, TS: now}); err != nil {
			return err
		}
		return tx.Prune(now)
	}); err != nil {
		t.Fatal(err)
	}
	identity, err := fleet.LoadOrCreateIdentity(filepath.Join(dir, "fleet"))
	if err != nil {
		t.Fatal(err)
	}
	if err := fleet.SaveViewers(filepath.Join(dir, "fleet"), []fleet.Viewer{{DeviceID: identity.DeviceID, Permissions: []string{"read"}}}); err != nil {
		t.Fatal(err)
	}
	srv := fleet.NewServer(st, identity, dir, d.Cfg)
	req := httptest.NewRequest("GET", "/v1/snapshot", nil)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{identity.Cert.Leaf}}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("snapshot: %d: %s", w.Code, w.Body.String())
	}
	var snap fleet.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Data.InboxItems) != 1 {
		t.Fatal("retained inbox missing from snapshot")
	}
	var item feed.InboxRow
	if err := json.Unmarshal(snap.Data.InboxItems[0], &item); err != nil {
		t.Fatal(err)
	}
	if item.Session.Adapter == "" || item.Session.ExternalID == "" {
		t.Fatalf("empty session key: %s", snap.Data.InboxItems[0])
	}
	viewer, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close()
	if err := viewer.Tx(context.Background(), func(tx *store.Tx) error {
		return fleet.ApplyBatch(tx, fleet.Batch{MachineID: st.MachineID(), Snapshot: &snap, Cursor: snap.Data.Cursor}, now)
	}); err != nil {
		t.Fatalf("snapshot apply rejected: %v", err)
	}
}

func TestShareDowngradeRotatesEpoch(t *testing.T) {
	for _, tc := range []struct{ name, share, capture, nextShare, nextCapture string }{
		{"snippets to titles", "snippets", "snippets", "titles", "snippets"},
		{"titles to none", "titles", "titles", "none", "titles"},
		{"capture downgrade", "snippets", "snippets", "snippets", "titles"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _, _, a := setup(t)
			path := filepath.Join(t.TempDir(), "driga.db")
			st, err := store.Open(path, store.ReadWrite)
			if err != nil {
				t.Fatal(err)
			}
			d.Cfg.Fleet.Serve = true
			d.Cfg.Fleet.ShareText, d.Cfg.CaptureText = tc.share, tc.capture
			a.obs[0].Title, a.obs[0].LastPrompt = "private title", "private prompt"
			pass(t, d, st, Plan{})
			epoch, _, seq, _ := st.FeedBounds()
			if err := st.Close(); err != nil {
				t.Fatal(err)
			}
			st, err = store.Open(path, store.ReadWrite)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			d.Cfg.Fleet.ShareText, d.Cfg.CaptureText = tc.nextShare, tc.nextCapture
			pass(t, d, st, Plan{})
			if _, err := st.FeedSince(epoch, seq, 0); !errors.Is(err, store.ErrResnapshot) {
				t.Fatalf("downgrade retained old epoch: %v", err)
			}
			current, min, max, _ := st.FeedBounds()
			rows, err := st.FeedSince(current, min-1, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				if row.Seq <= seq {
					t.Fatalf("old text row retained: %+v", row)
				}
			}
			level, err := st.Meta("feed_share_text")
			if err != nil || level != string(feed.EffectiveShare(feed.ShareText(tc.nextShare), tc.nextCapture)) {
				t.Fatalf("effective level not persisted: %q: %v", level, err)
			}
			// Restarts and widening policy keep the new cursor valid.
			d.Cfg.Fleet.ShareText, d.Cfg.CaptureText = "snippets", "snippets"
			pass(t, d, st, Plan{})
			if _, err := st.FeedSince(current, max, 0); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNoFeedWhenFleetUnused(t *testing.T) {
	d, st, _, _ := setup(t)
	pass(t, d, st, Plan{Procs: true, Machine: true})
	epoch, min, max, err := st.FeedBounds()
	if err != nil || epoch == "" || min != max+1 || max != 0 {
		t.Fatalf("unused fleet bounds: %q %d %d: %v", epoch, min, max, err)
	}
	// Authorized viewers count as consumers even while serving is disabled.
	if err := fleet.SaveViewers(filepath.Join(config.DataDir(), "fleet"), []fleet.Viewer{{DeviceID: "viewer"}}); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{Procs: true})
	_, _, max, _ = st.FeedBounds()
	if max == 0 {
		t.Fatal("authorized viewer did not enable feed")
	}
}

func TestEnableServeLaterResnapshots(t *testing.T) {
	d, st, _, a := setup(t)
	d.Cfg.Fleet.Serve = true
	pass(t, d, st, Plan{Procs: true})
	epoch, _, seq, _ := st.FeedBounds()
	d.Cfg.Fleet.Serve = false
	a.obs[0].Title = "changed while disabled"
	pass(t, d, st, Plan{})
	d.Cfg.Fleet.Serve = true
	pass(t, d, st, Plan{})
	if _, err := st.FeedSince(epoch, seq, 0); !errors.Is(err, store.ErrResnapshot) {
		t.Fatalf("old cursor resumed across disabled collection: %v", err)
	}
	// The current cursor remains valid, and local collection was preserved.
	current, _, max, err := st.FeedBounds()
	if err != nil || current == epoch {
		t.Fatalf("epoch did not rotate: %q: %v", current, err)
	}
	if _, err := st.FeedSince(current, max, 0); err != nil {
		t.Fatal(err)
	}
	ss, err := st.Sessions(store.SessionFilter{})
	if err != nil || len(ss) != 1 || ss[0].Title != a.obs[0].Title {
		t.Fatalf("local collection lost: %+v: %v", ss, err)
	}
}
