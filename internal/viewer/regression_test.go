package viewer

import (
	"context"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

func TestScopedOverviewDoesNotBuildDetails(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "one", LastActivityAt: time.Now()}, Label: "waiting"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	s := NewStoreSource(st, t.TempDir(), view.Filter{})
	snap, err := s.LoadScope(context.Background(), store.Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.AgentDetails) != 0 || len(snap.ProjectDetails) != 0 {
		t.Fatalf("overview built details: %d/%d", len(snap.AgentDetails), len(snap.ProjectDetails))
	}
}

func TestSnapshotHistoryPruning(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now()
	old := now.Add(-2 * time.Hour)
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		for _, at := range []time.Time{old, now} {
			if err := tx.InsertSample(model.MachineSample{TS: at}); err != nil {
				return err
			}
			if err := tx.InsertEvent(model.Event{Adapter: "test", ExternalID: "busy", TS: at, Kind: "tool"}); err != nil {
				return err
			}
		}
		return tx.PruneSnapshot(now)
	})
	if err != nil {
		t.Fatal(err)
	}
	samples, err := st.Samples(time.Time{})
	if err != nil || len(samples) != 1 {
		t.Fatalf("samples: %v %v", samples, err)
	}
	events, err := st.LatestEvents(time.Time{})
	if err != nil || len(events) != 1 {
		t.Fatalf("events: %v %v", events, err)
	}
	counts, err := st.EventCounts(old, now)
	if err != nil || len(counts) != 1 {
		t.Fatalf("minutes: %v %v", counts, err)
	}
}

func TestSnapshotIdleCadence(t *testing.T) {
	s := &LiveSnapshotSource{}
	now := time.Now()
	s.setActiveAt(false, now)
	if got := s.collectionInterval(now.Add(59 * time.Second)); got != 5*time.Second {
		t.Fatal(got)
	}
	if got := s.collectionInterval(now.Add(61 * time.Second)); got != time.Minute {
		t.Fatal(got)
	}
	s.setActiveAt(true, now.Add(62*time.Second))
	if got := s.collectionInterval(now.Add(63 * time.Second)); got != 5*time.Second {
		t.Fatal(got)
	}
}

type switchFixtureSource struct {
	mode    string
	version int64
	closed  bool
}

func (s *switchFixtureSource) Load(context.Context) (Snapshot, error) {
	return Snapshot{Envelope: view.Envelope{Mode: s.mode}}, nil
}
func (s *switchFixtureSource) LoadScope(ctx context.Context, _ store.Scope, _ ...bool) (Snapshot, error) {
	return s.Load(ctx)
}
func (s *switchFixtureSource) DataVersion(context.Context) (int64, error) { return s.version, nil }
func TestSourceRechecksRunningAndClosesOld(t *testing.T) {
	now := time.Now()
	running := false
	var sources []*switchFixtureSource
	s, err := NewSwitchingSource(context.Background(), func() bool { return running }, func(_ context.Context, daemon bool) (ScopedSource, func(), error) {
		mode := "snapshot"
		if daemon {
			mode = "daemon"
		}
		f := &switchFixtureSource{mode: mode, version: 1}
		sources = append(sources, f)
		return f, func() { f.closed = true }, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Now = func() time.Time { return now }
	snap, err := s.Load(context.Background())
	if err != nil || snap.Mode != "snapshot" {
		t.Fatal(snap, err)
	}
	first, _ := s.DataVersion(context.Background())
	running = true
	now = now.Add(6 * time.Second)
	snap, err = s.Load(context.Background())
	if err != nil || snap.Mode != "daemon" || !sources[0].closed {
		t.Fatal(snap, err, sources[0])
	}
	second, _ := s.DataVersion(context.Background())
	if second == first {
		t.Fatal("switch kept same token")
	}
	running = false
	now = now.Add(6 * time.Second)
	snap, err = s.Load(context.Background())
	if err != nil || snap.Mode != "snapshot" || !sources[1].closed {
		t.Fatal(snap, err, sources[1])
	}
	third, _ := s.DataVersion(context.Background())
	now = now.Add(16 * time.Second)
	fourth, _ := s.DataVersion(context.Background())
	if fourth == third {
		t.Fatal("time-dependent display does not refresh")
	}
}

type flowFixtureSource struct{ switchFixtureSource }

func (s *flowFixtureSource) LoadFlow(_ context.Context, _ store.Scope, id int64) (view.FlowView, error) {
	return view.FlowView{Lead: &view.FlowNode{ID: id}}, nil
}

// The web server always reads through a SwitchingSource, so the flow page
// is empty unless the wrapper forwards LoadFlow to its current source.
func TestSwitchingSourceForwardsFlow(t *testing.T) {
	s, err := NewSwitchingSource(context.Background(), func() bool { return true }, func(context.Context, bool) (ScopedSource, func(), error) {
		return &flowFixtureSource{switchFixtureSource{mode: "daemon", version: 1}}, func() {}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var src any = s
	fs, ok := src.(FlowSource)
	if !ok {
		t.Fatal("SwitchingSource does not implement FlowSource")
	}
	f, err := fs.LoadFlow(context.Background(), store.Scope{}, 5)
	if err != nil || f.Lead == nil || f.Lead.ID != 5 {
		t.Fatal(f, err)
	}
}

func TestOverviewDefersSessionDetailsToSelection(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var id int64
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		var err error
		id, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "selected", LastActivityAt: time.Now()}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	src := NewStoreSource(st, t.TempDir(), view.Filter{})
	snap, err := src.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.AgentDetails) != 0 {
		t.Fatal("overview eagerly loaded sessions")
	}
	detail, err := src.LoadAgent(context.Background(), store.Scope{}, id)
	if err != nil || detail.Session.ID != id {
		t.Fatal(detail, err)
	}
}
