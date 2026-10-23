package attention

import (
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
)

func TestPlanAlertsStartedFrom(t *testing.T) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	c := config.Default().Inbox
	c.Alerts = "all"
	for _, surface := range []string{"", "Fixture Editor"} {
		for _, kind := range []string{"waiting", "finished"} {
			s := model.Session{ID: 1, MachineID: "peer", StartedFrom: surface, SessionObs: model.SessionObs{Title: "Task", Hosted: true}}
			i := model.InboxItem{ID: "item", MachineID: "peer", SessionID: 1, Kind: kind, TS: at.Add(-time.Hour)}
			n := PlanAlerts([]model.InboxItem{i}, []model.Session{s}, c, "none", at, map[string]time.Time{}, map[string]time.Time{})
			if len(n) != 1 || strings.HasSuffix(n[0].Body, " · from Fixture Editor") != (surface != "") {
				t.Fatalf("%s: %+v", kind, n)
			}
		}
	}
}

func TestPlanAlertsGroupedStartedFrom(t *testing.T) {
	at := time.Now().UTC()
	ss := []model.Session{{ID: 1, StartedFrom: "Editor"}, {ID: 2, StartedFrom: "Terminal"}, {ID: 3, StartedFrom: "Editor"}}
	items := []model.InboxItem{}
	for _, s := range ss {
		items = append(items, model.InboxItem{ID: string(rune('a' + s.ID)), SessionID: s.ID, Kind: "waiting", TS: at.Add(-time.Minute)})
	}
	n := PlanAlerts(items, ss, config.Default().Inbox, "titles", at, map[string]time.Time{}, map[string]time.Time{})
	if len(n) != 1 || n[0].Body != "3 sessions waiting · from Editor, Terminal" {
		t.Fatal(n)
	}
}
