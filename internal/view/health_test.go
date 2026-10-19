package view

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

// Without a machine sample the summary must not claim zero swap and load.
func TestSummaryWithoutSampleSaysUnavailable(t *testing.T) {
	var out bytes.Buffer
	if err := Render(&out, SummaryView{Agents: 2}, RenderOpts{NoColor: true, Envelope: Envelope{Mode: "snapshot"}}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); strings.Contains(got, "swap") || strings.Contains(got, "load") || !strings.Contains(got, "health unavailable") {
		t.Fatalf("summary: %q", got)
	}
}

// A source that never succeeded is stale even in a snapshot.
func TestSnapshotFailedSourceIsStale(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now()
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.SetSourceOK("claude", now); err != nil {
			return err
		}
		return tx.SetSourceError("procs", "operation not permitted", now)
	}); err != nil {
		t.Fatal(err)
	}
	e, err := NewEnvelope(st, "snapshot", now, Intervals{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !e.Freshness["procs"].Stale || e.Freshness["claude"].Stale {
		t.Fatalf("freshness: %+v", e.Freshness)
	}
}

// A session seen only through its transcript has no start time; its age is
// unknown, not the ~292 years since the zero time.
func TestUnknownSessionAgeIsDash(t *testing.T) {
	if got := sessionAge(model.Session{}, time.Now()); got != 0 {
		t.Fatalf("age: %v", got)
	}
	if got := sessionAgeText(0); got != "—" {
		t.Fatalf("text: %q", got)
	}
}
