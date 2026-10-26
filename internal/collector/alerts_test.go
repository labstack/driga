package collector

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestAlertFleetAndLocalCapturePolicies(t *testing.T) {
	for _, local := range []string{"none", "titles", "snippets"} {
		for _, share := range []feed.ShareText{feed.None, feed.Titles, feed.Snippets} {
			t.Run(local+"/"+string(share), func(t *testing.T) {
				d, st, _, _ := setup(t)
				d.Cfg.CaptureText = local
				n := &fakeNotifier{}
				d.Notifier = n
				if err := fleet.SavePeers(filepath.Join(config.DataDir(), "fleet"), []fleet.Peer{{MachineID: "peer", DeviceID: "fixture"}}); err != nil {
					t.Fatal(err)
				}
				// Exercise actual fleet encoding before storing the peer's bounded data.
				_, sb := feed.Encode(feed.Session, model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "remote", Title: "Session title", Cwd: "/code/project"}}, share)
				_, ib := feed.Encode(feed.Inbox, model.InboxItem{ID: "wait", Kind: "waiting", TS: d.now(), Reason: "permission", Tool: "Bash", Detail: "private command"}, share)
				var s feed.SessionRow
				var i feed.InboxRow
				if err := json.Unmarshal(sb, &s); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(ib, &i); err != nil {
					t.Fatal(err)
				}
				if err := st.Tx(context.Background(), func(tx *store.Tx) error {
					r := tx.ForMachine("peer")
					id, err := r.UpsertSession(s.Session)
					if err != nil {
						return err
					}
					i.SessionID = id
					return r.ApplyInbox(i.InboxItem)
				}); err != nil {
					t.Fatal(err)
				}
				if err := alertInbox(context.Background(), d, st); err != nil {
					t.Fatal(err)
				}
				label, reason := "project", "permission"
				if local != "none" && share != feed.None {
					label, reason = "Session title", "permission (Bash)"
				}
				want := label + " · " + reason
				if local == "snippets" && share == feed.Snippets {
					want += " · private command"
				}
				if len(n.notes) != 1 || n.notes[0].Body != want || n.notes[0].ItemID != "wait" {
					t.Fatalf("notes=%+v; want %q with scoped app click", n.notes, want)
				}
			})
		}
	}
}

func TestAlertRemoteIDCollisionPreservesMachineTarget(t *testing.T) {
	d, st, _, _ := setup(t)
	n := &fakeNotifier{}
	d.Notifier = n
	at := d.now()
	if err := fleet.SavePeers(filepath.Join(config.DataDir(), "fleet"), []fleet.Peer{{MachineID: "peer", DeviceID: "fixture"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.UpsertInboxItem(model.InboxItem{ID: "same", Kind: "finished", TS: at}); err != nil {
			return err
		}
		return tx.ForMachine("peer").ApplyInbox(model.InboxItem{ID: "same", Kind: "waiting", TS: at.Add(-time.Minute)})
	}); err != nil {
		t.Fatal(err)
	}
	if err := alertInbox(context.Background(), d, st); err != nil {
		t.Fatal(err)
	}
	if len(n.notes) != 1 || n.notes[0].ItemID != "same" || n.notes[0].MachineID != "peer" {
		t.Fatal(n.notes)
	}
}
