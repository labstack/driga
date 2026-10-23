package view

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestFlowElapsedDisplayUsesSessionDuration(t *testing.T) {
	for _, ended := range []bool{false, true} {
		st := fixture(t, func(tx *store.Tx) error {
			s := model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "long-running", PID: 1, StartedAt: testNow.Add(-2765 * time.Minute)}, Label: "working"}
			if ended {
				end := testNow.Add(-time.Hour)
				s.EndedAt = &end
			}
			_, err := tx.UpsertSession(s)
			return err
		})
		f, err := Flow(st, Filter{}, 1, testNow)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(f.Lead)
		if err != nil {
			t.Fatal(err)
		}
		var wire struct {
			ElapsedText string `json:"elapsed_text"`
		}
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatal(err)
		}
		if want := SessionDuration(f.Lead.Elapsed); wire.ElapsedText != want {
			t.Errorf("ended=%t: elapsed display %q, want shared formatter %q", ended, wire.ElapsedText, want)
		}
	}
}
