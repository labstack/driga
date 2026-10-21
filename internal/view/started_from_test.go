package view

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestStartedFromSessionSurfaces(t *testing.T) {
	for _, surface := range []string{"", "Fixture Editor"} {
		t.Run(surface, func(t *testing.T) {
			var id int64
			st := fixture(t, func(tx *store.Tx) error {
				var err error
				id, err = tx.UpsertSession(model.Session{StartedFrom: surface, SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "surface", Title: "Task", LastActivityAt: testNow, StartedAt: testNow}, Label: "working"})
				return err
			})
			agents, err := Agents(st, Filter{}, testNow)
			if err != nil {
				t.Fatal(err)
			}
			detail, err := Agent(st, id, testNow)
			if err != nil {
				t.Fatal(err)
			}
			flow, err := Flow(st, Filter{}, id, testNow)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range []any{agents, detail, flow} {
				var out bytes.Buffer
				if err := Render(&out, v, RenderOpts{NoColor: true}); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(out.String(), " · from ") != (surface != "") {
					t.Fatalf("%T: %s", v, out.String())
				}
				b, err := json.Marshal(v)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(b, []byte(`"started_from"`)) != (surface != "") {
					t.Fatalf("%T JSON: %s", v, b)
				}
			}
			if strings.Contains(flow.Lead.DisplayTitle(), " · from ") != (surface != "") {
				t.Fatalf("Flow map label: %s", flow.Lead.DisplayTitle())
			}
		})
	}
}
