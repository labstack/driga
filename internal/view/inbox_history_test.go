package view

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/adapter/claude"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/fixturegen"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/spool"
)

func TestInboxCurrentWaitsAndBoundedCompletions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("DRIGA_DATA_DIR", filepath.Join(home, "data"))
	t.Setenv("DRIGA_SPOOL_DIR", filepath.Join(home, "spool"))
	t.Setenv("DRIGA_CONFIG", filepath.Join(home, "config.toml"))
	if err := fixturegen.Write(home, fixturegen.Scenario{Now: testNow, Repos: 1, Agents: []fixturegen.AgentSpec{
		{Adapter: "claude", ID: "current", Status: "idle"},
		{Adapter: "claude", ID: "gone", Status: "idle", Crashed: true},
		{Adapter: "claude", ID: "moved-on", Status: "idle"},
		{Adapter: "claude", ID: "completed", Status: "idle", ActivityAge: time.Hour},
	}}); err != nil {
		t.Fatal(err)
	}
	// These are the ordinary Stop/Notification spool records emitted by hooks.
	for n := range 200 {
		at := testNow.Add(-time.Duration(n+1) * time.Minute)
		if n >= 100 {
			at = at.Add(-48 * time.Hour)
		}
		if err := spool.Append(config.SpoolDir(), model.HookEvent{Adapter: "claude", SessionID: "moved-on", Event: "Stop", TS: at, Message: fmt.Sprintf("completed turn %d", n)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, wait := range []struct {
		id  string
		age time.Duration
	}{{"current", 0}, {"gone", 47 * time.Hour}, {"moved-on", 2*time.Minute + time.Second}} {
		if err := spool.Append(config.SpoolDir(), model.HookEvent{Adapter: "claude", SessionID: wait.id, Event: "Notification", NotificationType: "permission_prompt", TS: testNow.Add(-wait.age)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range []model.HookEvent{
		{Adapter: "claude", SessionID: "completed", Event: "Notification", NotificationType: "unknown_notification", TS: testNow.Add(-20 * time.Minute)},
		{Adapter: "claude", SessionID: "completed", Event: "Stop", TS: testNow.Add(-10 * time.Minute)},
	} {
		if err := spool.Append(config.SpoolDir(), h); err != nil {
			t.Fatal(err)
		}
	}
	st := fixture(t, nil)
	if err := st.SetMeta("baseline_at", testNow.Add(-7*24*time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	plat, err := platform.LoadFake(home)
	if err != nil {
		t.Fatal(err)
	}
	d := collector.Deps{Cfg: config.Default(), Plat: plat, Adapters: []adapter.Adapter{claude.New()}, SpoolDir: config.SpoolDir(), Now: func() time.Time { return testNow }, State: collector.NewState()}
	if _, err := collector.Pass(context.Background(), d, st, collector.Plan{Procs: true}); err != nil {
		t.Fatal(err)
	}
	before, err := st.Inbox(time.Time{}, true)
	if err != nil {
		t.Fatal(err)
	}
	v, err := Inbox(st, Filter{}, testNow)
	if err != nil || len(v.Rows) != 51 || v.Rows[0].Kind != "waiting" {
		t.Fatalf("default inbox must contain one current wait and 50 recent completions: rows=%d err=%v", len(v.Rows), err)
	}
	agents, err := Summary(st, Filter{}, testNow)
	if err != nil || agents.Waiting != 1 || WaitingHeader(v.Rows, testNow) != "1 waiting · oldest 0s" {
		t.Fatalf("waiting disagreement: %+v %s %v", agents, WaitingHeader(v.Rows, testNow), err)
	}
	history, err := Inbox(st, Filter{All: true}, testNow)
	if err != nil || len(history.Rows) != 205 {
		t.Fatalf("history lost: %d %v", len(history.Rows), err)
	}
	historical := 0
	for _, row := range history.Rows {
		if row.Historical {
			historical++
		}
		if row.Kind == "waiting" && strings.HasPrefix(row.ID, "claude:completed:") && (row.EndedAt == nil || row.AckedAt != nil) {
			t.Fatal("Stop must end a wait without acknowledging it")
		}
	}
	if historical != 3 {
		t.Fatalf("want three historical waits, got %d", historical)
	}
	after, err := st.Inbox(time.Time{}, true)
	if err != nil || len(before) != len(after) {
		t.Fatal("read changed inbox")
	}
	for i := range before {
		if (before[i].AckedAt == nil) != (after[i].AckedAt == nil) {
			t.Fatal("read changed acknowledgements")
		}
	}
}

func TestWaitingHeaderUsesAgentState(t *testing.T) {
	rows := []InboxRow{
		{MachineID: "local", InboxItem: model.InboxItem{SessionID: 1, Kind: "waiting", TS: testNow.Add(-47 * time.Hour)}},
		{MachineID: "peer", InboxItem: model.InboxItem{SessionID: 1, Kind: "waiting", TS: testNow.Add(-time.Minute)}},
		{MachineID: "peer", InboxItem: model.InboxItem{SessionID: 1, Kind: "waiting", TS: testNow.Add(-time.Minute)}},
	}
	agents := []AgentRow{
		{MachineID: "local", ID: 1, Label: "unknown"},
		{MachineID: "peer", ID: 1, Label: "waiting", Children: []AgentRow{{MachineID: "peer", ID: 2, Label: "waiting", LastActivityAt: testNow.Add(-2 * time.Minute)}}},
	}
	if got := AgentWaitingHeader(rows, agents, testNow); got != "2 waiting · oldest 2m0s" {
		t.Fatal(got)
	}
}
