package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/dashboard"
	"github.com/labstack/driga/internal/panel"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
)

type unavailableDashboardSource struct{ appSource }

func (unavailableDashboardSource) LoadScope(context.Context, store.Scope, ...bool) (viewer.Snapshot, error) {
	return viewer.Snapshot{}, errors.New("snapshot unavailable")
}

func TestDashboardSaveSurvivesUnavailableLiveData(t *testing.T) {
	saved := dashboard.New(t.TempDir())
	s, err := New(unavailableDashboardSource{}, Options{Dashboards: saved})
	if err != nil {
		t.Fatal(err)
	}
	w := postJSON(s, "/app/api/create", `{"dashboard":{"name":"Committed","panels":[{"id":"w","title":"W","viz":"stat","source":"inbox"}]}}`)
	var f panel.Fragment
	check := func(version int) {
		t.Helper()
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &f) != nil || f.ID == "" || f.Version != version || len(f.Results) != 1 || f.Results[0].Status != panel.StatusError || !strings.Contains(f.Results[0].Message, "live data unavailable") {
			t.Fatalf("committed save reported failure: %d %s", w.Code, w.Body)
		}
		r, err := saved.Get(f.ID)
		if err != nil || r.Version != version || r.Name != f.Dashboard.Name {
			t.Fatalf("save differs from response: %+v %v", r, err)
		}
	}
	check(1)
	w = postJSON(s, "/app/api/edit", fmt.Sprintf(`{"id":%q,"base_version":1,"operations":[{"op":"rename","name":"Edited"}]}`, f.ID))
	check(2)
	w = postJSON(s, "/app/api/restore", fmt.Sprintf(`{"id":%q,"base_version":2,"version":1}`, f.ID))
	check(3)
	if w := postJSON(s, "/app/api/open", fmt.Sprintf(`{"id":%q}`, f.ID)); w.Code != 400 {
		t.Fatalf("refresh did not report unavailable live data: %d %s", w.Code, w.Body)
	}
}

type attentionSource struct{ appSource }

func (attentionSource) LoadScope(context.Context, store.Scope, ...bool) (viewer.Snapshot, error) {
	return viewer.Snapshot{LocalMachineID: "a", Agents: view.AgentsView{Rows: []view.AgentRow{{MachineID: "a", ID: 7, Label: "waiting"}, {MachineID: "b", ID: 7, Label: "waiting"}}}}, nil
}

type attentionClient struct {
	chat.Client
	prompts []chat.Prompt
	err     error
}

func (c *attentionClient) Prompts(context.Context) ([]chat.Prompt, error)   { return c.prompts, c.err }
func (c *attentionClient) AnswerPrompt(context.Context, string, bool) error { return nil }

func TestHeaderServesMergedMemoryOnlyAttention(t *testing.T) {
	c := &attentionClient{prompts: []chat.Prompt{{SessionID: 7}, {SessionID: 8, Input: json.RawMessage(`{"command":"memory-only"}`)}, {SessionID: 8}, {}}}
	s, err := New(attentionSource{}, Options{Dashboards: dashboard.New(t.TempDir()), Chat: c})
	if err != nil {
		t.Fatal(err)
	}
	check := func(want float64) {
		t.Helper()
		w := postJSON(s, "/app/api/open", `{"id":"header"}`)
		var f panel.Fragment
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &f) != nil || f.Results[0].Frame.Rows[0][0] != want || strings.Contains(w.Body.String(), "memory-only") {
			t.Fatalf("merged attention: %d %s", w.Code, w.Body)
		}
	}
	check(4)
	c.err = errors.New("prompt transport unavailable")
	check(4) // A failed prompt read retains the last live projection.
	c.err, c.prompts = nil, nil
	check(2) // Answered hooks no longer contribute, including duplicate sessions.
	// No overlay reaches a fresh database-backed snapshot.
	snap, _ := s.source.LoadScope(context.Background(), store.Scope{})
	if len(snap.LivePrompts) != 0 || len(snap.Inbox.Rows) != 0 {
		t.Fatal("prompt overlay escaped into the source")
	}
}

func TestDashboardAPI(t *testing.T) {
	s, err := New(appSource{}, Options{Dashboards: dashboard.New(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	var list struct{ Dashboards []dashboard.Summary }
	if w := postJSON(s, "/app/api/list", `{}`); json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Dashboards) != len(panel.BuiltinIDs) || list.Dashboards[0].ID != panel.HeaderID {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	if w := postJSON(s, "/app/api/open", `{}`); w.Code != 404 {
		t.Errorf("open without id: %d", w.Code)
	}
	w := postJSON(s, "/app/api/create", `{"dashboard":{"name":"Mine","panels":[{"id":"w","title":"W","viz":"stat","source":"inbox"}]}}`)
	var created panel.Fragment
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &created) != nil || created.ID == "" || created.Version != 1 || len(created.Results) != 1 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	if w := postJSON(s, "/app/api/edit", `{"id":"header","operations":[{"op":"add","panel":{"id":"t","title":"T","viz":"table","source":"sessions"}}]}`); w.Code != 400 {
		t.Errorf("table in header: %d %s", w.Code, w.Body)
	}
	if w := postJSON(s, "/app/api/delete", `{"id":"`+created.ID+`"}`); w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if w := postJSON(s, "/app/api/open", `{"id":"`+created.ID+`"}`); w.Code != 404 {
		t.Errorf("open deleted: %d", w.Code)
	}
}

func TestDashboardOpenNativeWithoutOrigin(t *testing.T) {
	s, err := New(appSource{}, Options{Dashboards: dashboard.New(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "mygo://localhost/app/api/open", strings.NewReader(`{"id":"header"}`))
	r.RemoteAddr = "mygo:1"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var f panel.Fragment
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &f) != nil || f.ID != panel.HeaderID || len(f.Results) == 0 {
		t.Fatalf("native dashboard open: %d %s", w.Code, w.Body)
	}
}

func TestDashboardCatalogRoute(t *testing.T) {
	s, err := New(appSource{}, Options{Dashboards: dashboard.New(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "mygo://localhost/app/api/catalog", strings.NewReader(`{}`))
	r.RemoteAddr = "mygo:1"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var out struct {
		Widgets []panel.Panel `json:"widgets"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Widgets) != 18 || !reflect.DeepEqual(out.Widgets, panel.HeaderCatalog()) {
		t.Fatalf("native catalog: %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "mygo://localhost/app/api/catalog", nil)
	r.RemoteAddr = "mygo:1"
	s.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET catalog: %d %s", w.Code, w.Body)
	}
}

func TestHeaderUndoShippedSpecWithReplace(t *testing.T) {
	s, err := New(appSource{}, Options{Dashboards: dashboard.New(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	w := postJSON(s, "/app/api/edit", `{"id":"header","base_version":0,"operations":[{"op":"remove","id":"reclaim"}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("first edit: %d %s", w.Code, w.Body)
	}
	spec := panel.Header()
	body, _ := json.Marshal(dashboardRequest{ID: "header", BaseVersion: 1, Dashboard: &spec})
	r := httptest.NewRequest(http.MethodPost, "mygo://localhost/app/api/replace", strings.NewReader(string(body)))
	r.RemoteAddr = "mygo:1"
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var f panel.Fragment
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &f) != nil || f.Version != 2 || !reflect.DeepEqual(f.Dashboard, panel.Header()) {
		t.Fatalf("undo shipped header: %d %s", w.Code, w.Body)
	}
	if w := postJSON(s, "/app/api/replace", string(body)); w.Code != http.StatusConflict {
		t.Fatalf("stale undo: %d %s", w.Code, w.Body)
	}
	if w := postJSON(s, "/app/api/replace", `{"id":"header","base_version":2}`); w.Code != http.StatusBadRequest {
		t.Fatalf("missing spec: %d %s", w.Code, w.Body)
	}
}

func TestHeaderResetPreservesHistory(t *testing.T) {
	saved := dashboard.New(t.TempDir())
	s, err := New(appSource{}, Options{Dashboards: saved})
	if err != nil {
		t.Fatal(err)
	}
	w := postJSON(s, "/app/api/edit", `{"id":"header","base_version":0,"operations":[{"op":"remove","id":"reclaim"}]}`)
	if w.Code != 200 {
		t.Fatalf("edit: %d %s", w.Code, w.Body)
	}
	previous, err := saved.Get("header")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "mygo://localhost/app/api/reset", strings.NewReader(`{"id":"header","base_version":1}`))
	r.RemoteAddr = "mygo:1"
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var f panel.Fragment
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &f) != nil || f.Version != 2 || !reflect.DeepEqual(f.Dashboard, panel.Header()) {
		t.Fatalf("reset: %d %s", w.Code, w.Body)
	}
	versions, err := saved.Versions("header")
	if err != nil || len(versions) != 2 || versions[0].Message != "Reset to default" || versions[1].Version != 1 {
		t.Fatalf("history: %+v %v", versions, err)
	}
	w = postJSON(s, "/app/api/reset", `{"id":"header","base_version":1}`)
	if w.Code != 409 {
		t.Fatalf("stale reset: %d %s", w.Code, w.Body)
	}
	w = postJSON(s, "/app/api/restore", `{"id":"header","base_version":2,"version":1}`)
	// Decode a fresh response: omitted fields must not retain the reset spec's filters.
	f = panel.Fragment{}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &f) != nil || f.Version != 3 || !reflect.DeepEqual(f.Dashboard, previous.Spec) {
		t.Fatalf("restore previous: %d %s", w.Code, w.Body)
	}
	w = postJSON(s, "/app/api/reset", `{"id":"custom","base_version":0}`)
	if w.Code != 400 {
		t.Fatalf("non-builtin: %d %s", w.Code, w.Body)
	}
	r = httptest.NewRequest(http.MethodGet, "mygo://localhost/app/api/reset", nil)
	r.RemoteAddr = "mygo:1"
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 405 {
		t.Fatalf("GET reset: %d", w.Code)
	}
}
