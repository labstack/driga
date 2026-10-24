package tui

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/model"
)

func TestTUIShowsAckPending(t *testing.T) {
	s := fleetSource()
	s.snap.Inbox.Rows[0].MachineID = "remote"
	s.snap.Inbox.Rows[0].InboxItem.MachineID = "remote"
	dir := t.TempDir()
	var peer fleet.Peer
	json.Unmarshal([]byte(`{"name":"vm-1","machine_id":"remote","ack":true}`), &peer)
	if err := fleet.SavePeers(filepath.Join(dir, "fleet"), []fleet.Peer{peer}); err != nil {
		t.Fatal(err)
	}
	var events []model.HookEvent
	m := setup(t, s, Options{DataDir: dir, Spool: func(ev model.HookEvent) error { events = append(events, ev); return nil }})
	m = key(key(m, '0'), 'h')
	m, cmd := press(m, 'x')
	if cmd == nil {
		t.Fatalf("missing remote action: %s", m.View().Content)
	}
	m = execute(t, m, cmd)
	data, _ := json.Marshal(events)
	if len(events) != 1 || !strings.Contains(string(data), `"machine_id":"remote"`) || !strings.Contains(m.View().Content, "ack pending → vm-1") {
		t.Fatalf("%s: %s", data, m.View().Content)
	}
	// Pending is visible after loading an existing request, too.
	s.snap.Inbox.Rows[0].AckPendingAt = &s.snap.ObservedAt
	m = setup(t, s, Options{DataDir: dir})
	m = key(key(m, '0'), 'h')
	if !strings.Contains(m.View().Content, "ack pending → vm-1") {
		t.Fatal(m.View().Content)
	}
}
