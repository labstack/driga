package collector

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestServeOffOpensNoPort(t *testing.T) {
	d, _, _, a := setup(t)
	d.Cfg.Fleet.Serve = false
	d.Cfg.Fleet.Listen = "invalid address"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.collect = func(context.Context) { cancel() }
	dir := t.TempDir()
	if err := Run(ctx, d, dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"listen", "fleet.sock", "identity.key"} {
		if _, err := os.Stat(filepath.Join(dir, "fleet", name)); !os.IsNotExist(err) {
			t.Fatalf("serve off created %s: %v", name, err)
		}
	}
	// startFleet must also be a no-op without trying to open the invalid address.
	if server, stop := startFleetServer(ctx, d, dir, make(chan error, 1)); stop != nil || server != nil {
		if stop != nil {
			stop()
		}
		t.Fatal("serve off started fleet")
	}
}

func TestCollectorServesCommittedSpoolChanges(t *testing.T) {
	d, _, _, _ := setup(t)
	dir, err := os.MkdirTemp("/tmp", "driga-collector-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	d.Cfg.Fleet.Serve = true
	d.Cfg.Fleet.Listen = "127.0.0.1:0"
	// Precreate identities/trust only in isolated directories, as pairing is a
	// separate task. The collector still owns the only write connection.
	serverID, err := fleet.LoadOrCreateIdentity(filepath.Join(dir, "fleet"))
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := fleet.LoadOrCreateIdentity(filepath.Join(t.TempDir(), "fleet"))
	if err != nil {
		t.Fatal(err)
	}
	if err := fleet.SaveViewers(filepath.Join(dir, "fleet"), []fleet.Viewer{{DeviceID: viewer.DeviceID, Permissions: []string{"read"}}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, d, dir) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	deadline := time.After(5 * time.Second)
	var address string
	for {
		b, err := os.ReadFile(filepath.Join(dir, "fleet", "listen"))
		if err == nil {
			address = strings.TrimSpace(string(b))
			break
		}
		select {
		case <-deadline:
			t.Fatal("collector never listened")
		default:
			runtime.Gosched()
		}
	}
	pin, _ := fleet.ParseDeviceID(serverID.DeviceID)
	transport := &http.Transport{TLSClientConfig: fleet.PinnedTLS(viewer, func(fp [32]byte) bool { return fleet.EqualFingerprint(pin, fp) })}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	// Wait for the first committed collection by reading the DB, then resume at
	// its cursor. The first pass can be gathering while the listener starts.
	ro, err := store.Open(filepath.Join(dir, "driga.db"), store.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	for {
		if _, err := ro.Machine(); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("collector did not commit")
		default:
			runtime.Gosched()
		}
	}
	epoch, _, seq, err := ro.FeedBounds()
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", "https://"+address+"/v1/events", nil)
	req.Header.Set("Last-Event-ID", fmt.Sprintf("%s:%d", epoch, seq))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	if err := spool.Append(d.SpoolDir, model.HookEvent{TS: d.now(), Adapter: "claude", SessionID: "one", Event: "Notification"}); err != nil {
		t.Fatal(err)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "event: event\n" {
			break
		}
	}
}

func TestDrainOnlyPublishesAckAndHandoff(t *testing.T) {
	d, st, _, _ := setup(t)
	d.Cfg.Fleet.Serve = true
	d.Cfg.Fleet.Serve = true
	d.Cfg.CaptureText = "snippets"
	d.Cfg.Fleet.ShareText = "snippets"
	pass(t, d, st, Plan{Procs: true})
	id := sessions(t, st)[0].ID
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.UpsertInboxItem(model.InboxItem{ID: "item", SessionID: id, TS: d.now()})
	}); err != nil {
		t.Fatal(err)
	}
	_, _, seq, _ := st.FeedBounds()
	for _, h := range []model.HookEvent{{TS: d.now(), Event: "ack", Message: "item"}, {TS: d.now(), Adapter: "claude", SessionID: "one", Event: "Stop", Message: "handoff"}} {
		if err := spool.Append(d.SpoolDir, h); err != nil {
			t.Fatal(err)
		}
	}
	if err := drainOnly(context.Background(), d, st); err != nil {
		t.Fatal(err)
	}
	ack, handoff := false, false
	for _, row := range feedRows(t, st, seq) {
		if row.Kind == string(feed.Inbox) && strings.Contains(string(row.Payload), "acked_at") {
			ack = true
		}
		if row.Kind == string(feed.Handoff) {
			handoff = true
		}
	}
	if !ack || !handoff {
		t.Fatalf("ack=%t handoff=%t", ack, handoff)
	}
}

func TestServeFailureKeepsCollectorRunning(t *testing.T) {
	d, _, _, a := setup(t)
	d.Cfg.Fleet.Serve = true
	d.Cfg.Fleet.Listen = "8.8.8.8:7411"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.collect = func(context.Context) { cancel() }
	dir := t.TempDir()
	if err := Run(ctx, d, dir); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "driga.db"), store.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sources, err := st.Sources()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sources {
		if s.Name == "fleet" && strings.Contains(s.LastError, "allow_any_address") {
			return
		}
	}
	t.Fatal("fleet refusal not recorded")
}

func TestGapPublishesFeed(t *testing.T) {
	d, st, _, _ := setup(t)
	old := d.now().Add(-10 * time.Minute)
	if err := st.SetMeta("last_pass_at", old.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := recordGap(context.Background(), d, st); err != nil {
		t.Fatal(err)
	}
	for _, row := range feedRows(t, st, 0) {
		if row.Kind == string(feed.Gap) {
			return
		}
	}
	t.Fatal("restart gap not published")
}

func TestBadPeerBatchDoesNotFailOthers(t *testing.T) {
	d, st, _, _ := setup(t)
	dir := t.TempDir()
	f := newFleetPullers(dir)
	peers := []fleet.Peer{{MachineID: "good-before"}, {MachineID: "bad"}, {MachineID: "good-after"}}
	if err := fleet.SavePeers(filepath.Join(dir, "fleet"), peers); err != nil {
		t.Fatal(err)
	}
	f.stamp, _ = os.Stat(filepath.Join(dir, "fleet", "peers.json"))
	for i := range peers {
		p := peers[i]
		f.peers[p.MachineID] = &peerPull{peer: p, puller: fleet.NewPuller(p, fleet.Identity{}, f.batches)}
		b := fleet.Batch{MachineID: p.MachineID, Peer: &p, ContactAt: d.now()}
		if p.MachineID == "bad" {
			snap := &fleet.Snapshot{Schema: 1, ObservedAt: d.now()}
			snap.Data.Machine = []byte(`{"machine_id":"bad"}`)
			snap.Data.Cursor = fleet.Cursor{Epoch: "epoch"}
			snap.Data.Sessions = []json.RawMessage{json.RawMessage(`{"adapter":"claude","external_id":"partial"}`), json.RawMessage(`{}`)}
			b.Snapshot = snap
		}
		f.ready = append(f.ready, b)
	}
	if err := f.wake(context.Background(), d, st, true, false); err != nil {
		t.Fatal(err)
	}
	machines, err := st.Machines()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, m := range machines {
		got[m.MachineID] = true
	}
	if !got["good-before"] || !got["good-after"] || got["bad"] {
		t.Fatalf("peer commits: %v", got)
	}
	sessions, err := st.SessionsScoped(store.SessionFilter{IncludeEnded: true}, store.Scope{Machines: []string{"bad"}})
	if err != nil || len(sessions) != 0 {
		t.Fatalf("bad batch partially committed: %v %v", sessions, err)
	}
}
