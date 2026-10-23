package viewer

import (
	"context"
	"testing"
	"time"

	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

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
