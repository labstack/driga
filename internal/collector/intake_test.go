package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/labstack/driga/internal/intake"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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
	for i := 0; i < 2; i++ {
		resp, err := client.Post("http://"+addr+"/v1/logs", "application/json", strings.NewReader("{"))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatal(resp.StatusCode)
		}
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		rejected, err := st.Meta("otlp:rejected")
		if err == nil && rejected == "2" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("rejections not persisted: %q %v", rejected, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, key := range []string{"parse_failures", "reject:invalid json"} {
		if n, err := st.Meta("otlp:" + key); err != nil || n != "2" {
			t.Fatal(key, n, err)
		}
	}
	// A well-formed export for a session the adapters haven't discovered yet
	// is rejected with partial success without creating a session.
	raw, err := os.ReadFile("../../testdata/fixtures/otlp/claude-code-2.1.303/05_v1_logs.json")
	if err != nil {
		t.Fatal(err)
	}
	resp, err = client.Post("http://"+addr+"/v1/logs", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 || !strings.Contains(string(body), `"rejectedLogRecords":"1"`) || !strings.Contains(string(body), `"errorMessage":"unmatched sessions"`) {
		t.Fatal(resp.StatusCode, string(body), err)
	}
	if n, err := st.Meta("otlp:unmatched"); err != nil || n != "1" {
		t.Fatal(n, err)
	}
	// Mix one known and one unknown session in the same scope.
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	scope := doc["resourceLogs"].([]any)[0].(map[string]any)["scopeLogs"].([]any)[0].(map[string]any)
	var known map[string]any
	if err := json.Unmarshal(bytes.ReplaceAll(raw, []byte("fixture-session"), []byte("one")), &known); err != nil {
		t.Fatal(err)
	}
	records := known["resourceLogs"].([]any)[0].(map[string]any)["scopeLogs"].([]any)[0].(map[string]any)["logRecords"].([]any)
	scope["logRecords"] = append(scope["logRecords"].([]any), records...)
	mixed, _ := json.Marshal(doc)
	resp, err = client.Post("http://"+addr+"/v1/logs", "application/json", bytes.NewReader(mixed))
	if err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 || !bytes.Contains(body, []byte(`"rejectedLogRecords":"1"`)) {
		t.Fatal(resp.StatusCode, string(body), err)
	}
	metrics := `{"resourceMetrics":[{"scopeMetrics":[{"metrics":[{"name":"claude_code.token.usage","sum":{"dataPoints":[{"asInt":"12"},{"asInt":"34"}]}}]}]}]}`
	resp, err = client.Post("http://"+addr+"/v1/metrics", "application/json", strings.NewReader(metrics))
	if err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 || !bytes.Contains(body, []byte(`"rejectedDataPoints":"2"`)) || !bytes.Contains(body, []byte(`"errorMessage":"metrics are not used"`)) {
		t.Fatal(resp.StatusCode, string(body), err)
	}
	for key, want := range map[string]string{"accepted": "3", "unmatched": "2", "metrics_rejected": "2", "records_rejected": "4"} {
		if got, err := st.Meta("otlp:" + key); err != nil || got != want {
			t.Fatalf("%s = %q; want %q (%v)", key, got, want, err)
		}
	}
	ss, err = st.Sessions(store.SessionFilter{IncludeEnded: true})
	if err != nil || len(ss) != 1 {
		t.Fatal(ss, err)
	}
}

func TestIntakeRejectionsCoalesce(t *testing.T) {
	d, st, _, _ := setup(t)
	d.Cfg.Intake.OTLP = true
	d.Cfg.Intake.Listen = "127.0.0.1:0"
	requests := make(chan intakeRequest, 1)
	stop, err := startIntake(context.Background(), d, st, requests)
	if err != nil || stop == nil {
		t.Fatalf("start: %v", err)
	}
	defer stop()
	addr, err := st.Meta("otlp:listen")
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < 3; i++ {
		resp, err := client.Post("http://"+addr+"/v1/logs", "application/json", strings.NewReader("{"))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatal(resp.StatusCode)
		}
	}
	// Only the first notification fits. It must read all three failures.
	select {
	case req := <-requests:
		if req.stats == nil {
			t.Fatal("missing stats")
		}
		if err := recordIntakeStats(context.Background(), st, req.stats()); err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no rejection notification")
	}
	if n, err := st.Meta("otlp:reject:invalid json"); err != nil || n != "3" {
		t.Fatal(n, err)
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

func TestIntakeClaudeCode21303Fixtures(t *testing.T) {
	d, st, _, a := setup(t)
	a.obs[0].ExternalID = "fixture-session"
	a.obs[0].RegistryStatus = "idle"
	if _, err := Pass(context.Background(), d, st, Plan{}); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob("../../testdata/fixtures/otlp/claude-code-2.1.303/[0-9]*.json")
	if err != nil || len(files) != 7 {
		t.Fatal(files, err)
	}
	want := map[string]int{"mcp_server_connection": 4, "user_prompt": 1, "api_request": 2, "assistant_response": 1, "tool_result": 1, "tool_decision": 1}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		b, err := intake.Decode(bytes.NewReader(raw), "/v1/logs")
		if err != nil {
			t.Fatal(err)
		}
		if unmatched, err := applyIntake(context.Background(), d, st, b); err != nil || unmatched != 0 {
			t.Fatal(unmatched, err)
		}
	}
	events, err := st.Events("claude", "fixture-session", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]int)
	for _, e := range events {
		got[strings.TrimPrefix(e.Kind, "otlp:")]++
	}
	for name, count := range want {
		if got[name] != count {
			t.Fatalf("events: %v; want %v", got, want)
		}
	}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	ss, err := st.Sessions(store.SessionFilter{})
	if err != nil || len(ss) != 1 || ss[0].Label != "working" || ss[0].LastPromptAt.IsZero() || ss[0].LastActivityAt.Before(ss[0].LastPromptAt) {
		t.Fatal(ss, err)
	}
	state, err := st.IntakeState(ss[0].ID)
	if err != nil || state.LastAPIRequest.IsZero() {
		t.Fatal(state, err)
	}
	for key, want := range map[string]string{"received": "21", "ignored": "11", "invalid_records": "0", "unmatched": "0"} {
		if got, err := st.Meta("otlp:" + key); err != nil || got != want {
			t.Fatal(key, got, err)
		}
	}
	stored, _ := json.Marshal(events)
	for _, content := range []string{"<REDACTED>", "synthetic-user", "synthetic-organization"} {
		if bytes.Contains(stored, []byte(content)) {
			t.Fatal(string(stored))
		}
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

func TestIntakeListeningIsNotAReceivedEvent(t *testing.T) {
	d, st, _, _ := setup(t)
	d.Cfg.Intake.OTLP = true
	d.Cfg.Intake.Listen = "127.0.0.1:0"
	stop, err := startIntake(context.Background(), d, st, make(chan intakeRequest, 16))
	if err != nil || stop == nil {
		t.Fatalf("start: %v", err)
	}
	defer stop()
	sources, err := st.Sources()
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if source.Name == "otlp" && !source.LastOKAt.IsZero() {
			t.Fatal("listening incorrectly records a received event")
		}
	}
	if _, err := applyIntake(context.Background(), d, st, intake.Batch{}); err != nil {
		t.Fatal(err)
	}
	sources, err = st.Sources()
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if source.Name == "otlp" && !source.LastOKAt.IsZero() {
			t.Fatal("an empty export is not a received event")
		}
	}
	if _, err := applyIntake(context.Background(), d, st, intake.Batch{Facts: []intake.Fact{{SessionID: "unknown", Name: "user_prompt", TS: d.now()}}}); err != nil {
		t.Fatal(err)
	}
	sources, err = st.Sources()
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if source.Name == "otlp" && !source.LastOKAt.Equal(d.now()) {
			t.Fatal("received batch must record successful intake")
		}
	}
}

func TestFlowMetadataStoredWithoutContent(t *testing.T) {
	for _, capture := range []string{"none", "titles", "snippets"} {
		t.Run(capture, func(t *testing.T) {
			d, st, _, _ := setup(t)
			d.Cfg.CaptureText = capture
			if _, err := Pass(context.Background(), d, st, Plan{}); err != nil {
				t.Fatal(err)
			}
			raw := `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"claude-code"}}]},"scopeLogs":[{"logRecords":[{"timeUnixNano":"` + strconv.FormatInt(d.now().UnixNano(), 10) + `","attributes":[{"key":"session.id","value":{"stringValue":"one"}},{"key":"event.name","value":{"stringValue":"api_request"}},{"key":"model","value":{"stringValue":"claude-test"}},{"key":"input_tokens","value":{"intValue":"120"}},{"key":"cost_usd","value":{"doubleValue":0.12}},{"key":"prompt","value":{"stringValue":"PRIVATE_PROMPT"}},{"key":"response","value":{"stringValue":"PRIVATE_RESPONSE"}}],"body":{"stringValue":"PRIVATE_BODY"}}]}]}]}`
			b, err := intake.Decode(strings.NewReader(raw), "/v1/logs")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = applyIntake(context.Background(), d, st, b); err != nil {
				t.Fatal(err)
			}
			events, err := st.Events("claude", "one", time.Time{})
			if err != nil || len(events) == 0 {
				t.Fatal(events, err)
			}
			m := events[len(events)-1].Metadata
			if m == nil || m.Model != "claude-test" || m.InputTokens == nil || *m.InputTokens != 120 {
				t.Fatal(m)
			}
			sessions, _ := st.Sessions(store.SessionFilter{})
			stored, _ := json.Marshal([]any{events, sessions})
			if strings.Contains(string(stored), "PRIVATE_") {
				t.Fatal("OTLP content retained")
			}
			if sessions[0].Mixer == nil || sessions[0].Mixer.Verdict != sessions[0].Label {
				t.Fatal("mixer not stored with label")
			}
		})
	}
}
