package view

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/flowtest"
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

func TestFlowAttentionDistinguishesSessionsAndHonorsCapture(t *testing.T) {
	for _, policy := range []string{"titles", "snippets", "none"} {
		t.Run(policy, func(t *testing.T) {
			st := fixture(t, flowtest.SeedAmbiguousAttention)
			f, err := Flow(st, Filter{CaptureText: policy}, 0, flowtest.Now)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"sample · Fix cache", "sample · Fix parser", "sample · #ccc333", "sample · #ddd444"}
			if policy == "none" {
				want[0], want[1] = "sample · work/cache", "sample · work/parser"
			}
			if len(f.Attention.Waits) != len(want) {
				t.Fatal(f.Attention)
			}
			for i, row := range f.Attention.Waits {
				if row.Title != want[i] {
					t.Errorf("card %d: %q, want %q", i, row.Title, want[i])
				}
			}
			if policy == "none" {
				raw, err := json.Marshal(f.Attention)
				if err != nil || strings.Contains(string(raw), "Fix ") {
					t.Fatalf("capture none exposed title: %s (%v)", raw, err)
				}
			}
		})
	}
}
