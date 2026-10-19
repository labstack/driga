package collector

import (
	"context"
	"github.com/labstack/driga/internal/intake"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIntakeOffAndPortBusy(t *testing.T) {
	d, st, _, _ := setup(t)
	requests := make(chan intakeRequest, 16)
	stop, err := startIntake(context.Background(), d, st, requests)
	if err != nil || stop != nil {
		t.Fatalf("stop present=%v, error=%v", stop != nil, err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	d.Cfg.Intake.OTLP = true
	d.Cfg.Intake.Listen = ln.Addr().String()
	stop, err = startIntake(context.Background(), d, st, requests)
	if err != nil || stop != nil {
		t.Fatalf("stop present=%v, error=%v", stop != nil, err)
	}
	sources, err := st.Sources()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range sources {
		if s.Name == "otlp" && s.LastError != "" {
			found = true
		}
	}
	if !found {
		t.Fatal(sources)
	}
	if _, err := Pass(context.Background(), d, st, Plan{}); err != nil {
		t.Fatal(err)
	}
}

func TestIntakeDaemonHTTP(t *testing.T) {
	d, _, _, _ := setup(t)
	dir := t.TempDir()
	d.Cfg.Intake.OTLP = true
	d.Cfg.Intake.Listen = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, d, dir) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("collector did not stop")
		}
	})
	var addr string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, err := store.Open(filepath.Join(dir, "driga.db"), store.ReadOnly)
		if err == nil {
			ss, _ := st.Sessions(store.SessionFilter{})
			addr, _ = st.Meta("otlp:listen")
			st.Close()
			if len(ss) > 0 && addr != "" {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if addr == "" {
		t.Fatal("no intake listener")
	}
	b, err := os.ReadFile("../intake/testdata/claude/events.json")
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post("http://"+addr+"/v1/logs", "application/json", strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	st, err := store.Open(filepath.Join(dir, "driga.db"), store.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ss, _ := st.Sessions(store.SessionFilter{})
	if len(ss) != 1 || ss[0].ExternalID != "one" || ss[0].PID != 101 {
		t.Fatal(ss)
	}
	events, err := st.Events("claude", "one", time.Time{})
	if err != nil || len(events) != 2 {
		t.Fatal(events, err)
	}
	for _, e := range events {
		if strings.Contains(e.Summary, "NEVER_STORE") {
			t.Fatal(e)
		}
	}
	dropped, _ := st.Meta("otlp:dropped")
	if dropped != "6" {
		t.Fatal(dropped)
	}
}

func TestIntakePromptAndSpanCloseAttention(t *testing.T) {
	d, st, _, a := setup(t)
	a.obs[0].RegistryStatus = "idle"
	a.obs[0].LastActivityAt = d.now().Add(-time.Hour)
	if _, err := Pass(context.Background(), d, st, Plan{}); err != nil {
		t.Fatal(err)
	}
	ss, _ := st.Sessions(store.SessionFilter{})
	id := ss[0].ID
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.UpsertInboxItem(model.InboxItem{ID: "waiting", SessionID: id, Kind: "waiting", TS: d.now().Add(-time.Minute)})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := applyIntake(context.Background(), d, st, intake.Batch{Facts: []intake.Fact{{SessionID: "one", Name: "user_prompt", TS: d.now()}}}); err != nil {
		t.Fatal(err)
	}
	ss, _ = st.Sessions(store.SessionFilter{})
	if ss[0].Label != "working" || !ss[0].TurnOpen || !ss[0].LastPromptAt.Equal(d.now()) || !ss[0].LastActivityAt.Equal(d.now()) {
		t.Fatal(ss[0])
	}
	items, _ := st.Inbox(time.Time{}, false)
	if len(items) != 0 {
		t.Fatal(items)
	}
	if _, err := Pass(context.Background(), d, st, Plan{}); err != nil {
		t.Fatal(err)
	}
	ss, _ = st.Sessions(store.SessionFilter{})
	if ss[0].Label != "working" {
		t.Fatal(ss[0])
	}
	if _, err := applyIntake(context.Background(), d, st, intake.Batch{Facts: []intake.Fact{{SessionID: "one", Name: "tool.blocked_on_user", TS: d.now(), Start: d.now().Add(-time.Second)}}}); err != nil {
		t.Fatal(err)
	}
	ss, _ = st.Sessions(store.SessionFilter{})
	if ss[0].Label == "waiting" {
		t.Fatal(ss[0])
	}
}

func TestIntakeNeverCreatesSessions(t *testing.T) {
	d, st, _, _ := setup(t)
	n, e := applyIntake(context.Background(), d, st, intake.Batch{Facts: []intake.Fact{{SessionID: "absent", Name: "user_prompt", TS: d.now()}}})
	if e != nil || n != 1 {
		t.Fatal(n, e)
	}
	ss, _ := st.Sessions(store.SessionFilter{IncludeEnded: true})
	if len(ss) != 0 {
		t.Fatal(ss)
	}
	v, _ := st.Meta("otlp:unmatched")
	if v != "1" {
		t.Fatal(v)
	}
}
func TestIntakeAnnotatesAndSurvivesPass(t *testing.T) {
	d, st, _, a := setup(t)
	a.obs[0].LastActivityAt = d.now().Add(-20 * time.Minute)
	if _, e := Pass(context.Background(), d, st, Plan{}); e != nil {
		t.Fatal(e)
	}
	ts := d.now().Add(-time.Minute)
	if e := st.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.InsertEvent(model.Event{Adapter: "claude", ExternalID: "one", Kind: "Notification", TS: ts})
	}); e != nil {
		t.Fatal(e)
	}
	b := intake.Batch{Facts: []intake.Fact{{SessionID: "one", Name: "tool_decision", Source: "config", TS: d.now()}}}
	if _, e := applyIntake(context.Background(), d, st, b); e != nil {
		t.Fatal(e)
	}
	ss, _ := st.Sessions(store.SessionFilter{})
	if ss[0].Label != "waiting" {
		t.Fatal(ss[0])
	}
	b.Facts[0].Source = "user_temporary"
	b.Facts = append(b.Facts, intake.Fact{SessionID: "one", Name: "api_request", TS: d.now().Add(-3 * time.Second)})
	if _, e := applyIntake(context.Background(), d, st, b); e != nil {
		t.Fatal(e)
	}
	if _, e := Pass(context.Background(), d, st, Plan{}); e != nil {
		t.Fatal(e)
	}
	ss, _ = st.Sessions(store.SessionFilter{})
	if ss[0].Label != "working" || !strings.Contains(ss[0].Evidence, "API call") {
		t.Fatal(ss[0])
	}
}
