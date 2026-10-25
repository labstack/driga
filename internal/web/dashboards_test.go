package web

import (
	"encoding/json"
	"testing"

	"github.com/labstack/driga/internal/dashboard"
	"github.com/labstack/driga/internal/panel"
)

func TestDashboardAPI(t *testing.T) {
	s, err := New(appSource{}, Options{Dashboards: dashboard.New(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	var list struct{ Dashboards []dashboard.Summary }
	if w := postJSON(s, "/app/api/list", `{}`); json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Dashboards) != 1 || list.Dashboards[0].ID != panel.HeaderID {
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
