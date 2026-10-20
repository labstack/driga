// Package flowtest supplies a stored snapshot shared by flow regression tests.
package flowtest

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

var Now = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// SeedDefault supplies the same competing leads to CLI, TUI and web tests.
// The finished fanout is newest; the waiting family needs attention first.
func SeedDefault(tx *store.Tx) (int64, error) {
	var want int64
	for i, label := range []string{"working", "waiting", "waiting", "stalled", "finished"} {
		id, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{
			Adapter: "claude", ExternalID: fmt.Sprintf("default-%d", i), Origin: "interactive",
			PID: int32(100 + i), Title: "default " + label, LastActivityAt: Now.Add(time.Duration(i-5) * time.Minute),
		}, Label: label})
		if err != nil {
			return 0, err
		}
		if i == 2 {
			want = id
		}
		if _, err := tx.UpsertSession(model.Session{ParentID: &id, SessionObs: model.SessionObs{
			Adapter: "codex", ExternalID: fmt.Sprintf("default-worker-%d", i), Hosted: true, LastActivityAt: Now,
		}, Label: "waiting"}); err != nil {
			return 0, err
		}
	}
	_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{
		Adapter: "claude", ExternalID: "solitary", Origin: "interactive", PID: 200, LastActivityAt: Now,
	}, Label: "waiting"})
	return want, err
}

func Input() derive.StateInput {
	stop := Now.Add(-time.Minute)
	tool := Now.Add(-2 * time.Minute)
	return derive.StateInput{
		Obs:  model.SessionObs{Adapter: "claude", ExternalID: "flow-regression", PID: 321, RegistryStatus: "idle", LastActivityAt: Now.Add(-3 * time.Minute)},
		Proc: &model.Proc{PID: 321, CPUPercent: 8.4}, LastStop: &stop, LastToolResult: &tool, Unseen: true,
	}
}

func Expected() model.StateMixer {
	return derive.LabelWithEvidence(Input(), Now, derive.Thresholds{StalledAfter: 20 * time.Minute})
}

// Seed models a cached mixer from older derivation code, alongside the inputs
// of the label pass. Later process and telemetry samples must not replace them.
func Seed(tx *store.Tx) (int64, error) {
	in := Input()
	mixed := Expected()
	mixed.Agreement = 1
	mixed.Rows[3].Reading = "alive · CPU 0.0%"
	zero := 0.0
	mixed.Rows[3].CPUPercent = &zero
	raw, err := json.Marshal(mixed)
	if err != nil {
		return 0, err
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return 0, err
	}
	// Use the wire shape so this fixture also compiles before inputs are retained.
	fields["input"] = in
	fields["stalled_after_ms"] = (20 * time.Minute).Milliseconds()
	raw, err = json.Marshal(fields)
	if err != nil {
		return 0, err
	}
	if err := json.Unmarshal(raw, &mixed); err != nil {
		return 0, err
	}
	id, err := tx.UpsertSession(model.Session{SessionObs: in.Obs, Label: "finished", Evidence: Expected().Evidence, Mixer: &mixed})
	if err != nil {
		return 0, err
	}
	for i := 0; i < 18; i++ {
		obs := model.SessionObs{Adapter: "codex", ExternalID: fmt.Sprint("worker-", i), Hosted: true, LastActivityAt: Now.Add(-time.Hour + time.Second)}
		if i >= 5 {
			obs.LastActivityAt = Now.Add(-2 * time.Hour)
		}
		if _, err := tx.UpsertSession(model.Session{ParentID: &id, SessionObs: obs, Label: "idle"}); err != nil {
			return 0, err
		}
	}
	if err := tx.ReplaceProcs([]model.Proc{{PID: in.Obs.PID, CPUPercent: 0}}); err != nil {
		return 0, err
	}
	return id, tx.InsertEvent(model.Event{Adapter: in.Obs.Adapter, ExternalID: in.Obs.ExternalID, Kind: "otlp:api_request", TS: Now.Add(time.Second)})
}
