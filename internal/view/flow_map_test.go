package view

import (
	"github.com/labstack/driga/internal/flowtest"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"testing"
	"time"
)

func TestFlowMapStoredContract(t *testing.T) {
	var id int64
	ack := testNow.Add(-2 * time.Minute)
	st := fixture(t, func(tx *store.Tx) (err error) {
		id, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "map", PID: 1, StartedAt: testNow.Add(-8 * time.Hour)}, Label: "working"})
		if err != nil {
			return
		}
		for i := 0; i < 140; i++ {
			err = tx.InsertEvent(model.Event{Adapter: "claude", ExternalID: "map", Kind: "otlp:tool_result", TS: testNow.Add(-time.Duration(140-i) * time.Second)})
			if err != nil {
				return
			}
		}
		if err = tx.Observe(id, testNow.Add(-20*time.Minute), 300, 25); err != nil {
			return
		}
		return tx.UpsertInboxItem(model.InboxItem{ID: "ask", SessionID: id, Kind: "waiting", TS: testNow.Add(-5 * time.Minute), WaitingSinceAt: testNow.Add(-6 * time.Minute), AckedAt: &ack})
	})
	v, err := Flow(st, Filter{FlowMinutes: 240}, id, testNow)
	if err != nil {
		t.Fatal(err)
	}
	n := v.Lead
	if n.StartedAt != testNow.Add(-8*time.Hour) || len(n.Events) != 100 || len(n.Waits) != 1 || !n.Waits[0].EndedAt.Equal(ack) {
		t.Fatalf("stored node: %+v", n)
	}
	if n.Observed == nil || len(n.Observed.LogBytesPerMin) != 240 || !n.Observed.FromAt.Equal(testNow.Truncate(time.Minute).Add(-239*time.Minute)) {
		t.Fatal(n.Observed)
	}
	if len(v.Attention.Waits) != 0 {
		t.Fatal("acked wait in attention")
	}
	if !n.RetainedFromAt.Equal(testNow.Add(-140 * time.Second)) {
		t.Fatal(n.RetainedFromAt)
	}
	g := FlowGeometry(v, 200, false)
	if len(g.Rows) != 1 || len(g.Rows[0].Tools) == 0 || g.Rows[0].Hatched[0].To < 150 {
		t.Fatalf("geometry: %+v", g)
	}
}

func TestEightHourSnapshotFamily(t *testing.T) {
	var id int64
	st := fixture(t, func(tx *store.Tx) (err error) { id, err = flowtest.SeedMap(tx, testNow, true); return err })
	v, err := Flow(st, Filter{FlowMinutes: 240}, id, testNow)
	if err != nil {
		t.Fatal(err)
	}
	g := FlowGeometry(v, 240, false)
	if len(v.Children) != 16 || g.Quiet != 12 || len(g.Rows) != 5 {
		t.Fatalf("family rows=%d quiet=%d", len(g.Rows), g.Quiet)
	}
	if g.Rows[0].Hatched[0].To < 180 {
		t.Fatal("snapshot did not hatch most of the window", g.Rows[0].Hatched)
	}
	if len(v.Lead.Events) != 100 || len(v.Attention.Waits) != 1 || len(v.Gaps) != 1 {
		t.Fatal(v)
	}
	if v.Children[12].ParentID == nil || *v.Children[12].ParentID != id {
		t.Fatal("lost parent")
	}
}

func TestFlowSessionHandoffsAndMachineAttention(t *testing.T) {
	var lead, child int64
	st := fixture(t, func(tx *store.Tx) (err error) {
		repo, e := tx.UpsertRepo(model.Repo{Name: "flow", Path: "/flow"})
		if e != nil {
			return e
		}
		lead, err = tx.UpsertSession(model.Session{RepoID: &repo, SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "lead", PID: 1}, Label: "working"})
		if err != nil {
			return
		}
		child, err = tx.UpsertSession(model.Session{RepoID: &repo, ParentID: &lead, SessionObs: model.SessionObs{Adapter: "codex", ExternalID: "child"}, Label: "unknown"})
		if err != nil {
			return
		}
		other, e := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "codex", ExternalID: "other"}, Label: "stuck"})
		if e != nil {
			return e
		}
		if e = tx.InsertHandoff(other, testNow.Add(-time.Minute), "other completion"); e != nil {
			return e
		}
		if e = tx.InsertHandoff(child, testNow.Add(-2*time.Minute), "child completion"); e != nil {
			return e
		}
		return tx.UpsertInboxItem(model.InboxItem{ID: "other-ask", SessionID: other, Kind: "waiting", Reason: "permission", TS: testNow.Add(-3 * time.Minute), Detail: "must not escape titles"})
	})
	v, err := Flow(st, Filter{Project: "/flow", CaptureText: "titles"}, lead, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Attention.Waits) != 1 || v.Attention.Waits[0].Detail != "" || v.Attention.Working != 1 {
		t.Fatal(v.Attention)
	}
	if len(v.Children) != 1 || len(v.Children[0].Handoffs) != 1 || v.Children[0].Handoffs[0].Text != "child completion" {
		t.Fatal(v.Children)
	}
}

func TestFlowPreRetentionNeedsOneColumn(t *testing.T) {
	n := FlowNode{ID: 1, StartedAt: testNow.Add(-time.Hour), RetainedFromAt: testNow.Add(-time.Hour + 30*time.Second)}
	g := FlowGeometry(FlowView{Lead: &n, ObservedAt: testNow, WindowMinutes: 60}, 60, false)
	if len(g.Rows[0].Hatched) != 0 {
		t.Fatal("sub-column pre-retention hatch", g.Rows[0].Hatched)
	}
}

func TestFlowAttentionLongestWaitFirst(t *testing.T) {
	st := fixture(t, func(tx *store.Tx) error {
		for _, w := range []model.InboxItem{
			{ID: "new", SessionID: 1, Kind: "waiting", TS: testNow.Add(-time.Hour), WaitingSinceAt: testNow.Add(-time.Minute)},
			{ID: "old", SessionID: 2, Kind: "waiting", TS: testNow.Add(-time.Minute), WaitingSinceAt: testNow.Add(-2 * time.Hour)},
		} {
			if err := tx.UpsertInboxItem(w); err != nil {
				return err
			}
		}
		return nil
	})
	f, err := Flow(st, Filter{}, 0, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Attention.Waits) != 2 || f.Attention.Waits[0].Waited != 2*time.Hour {
		t.Fatal(f.Attention.Waits)
	}
}

func TestFlowHatchesStopAtSessionEnd(t *testing.T) {
	end := testNow.Add(-20 * time.Minute)
	n := FlowNode{ID: 1, StartedAt: testNow.Add(-time.Hour), EndedAt: &end}
	g := FlowGeometry(FlowView{Lead: &n, ObservedAt: testNow, WindowMinutes: 60}, 60, false)
	if len(g.Rows[0].Hatched) != 1 || g.Rows[0].Hatched[0].To > 41 {
		t.Fatal("hatch after session ended", g.Rows[0].Hatched)
	}
}

func TestFlowHatchesDoNotHideBoundaryMeasurements(t *testing.T) {
	n := FlowNode{ID: 1, StartedAt: testNow.Add(-time.Hour), RetainedFromAt: testNow.Add(-59 * time.Minute), Observed: &FlowObserved{FromAt: testNow.Add(-59 * time.Minute), LogBytesPerMin: []int64{10}}}
	g := FlowGeometry(FlowView{Lead: &n, ObservedAt: testNow, WindowMinutes: 60}, 60, false)
	if len(g.Rows[0].Hatched) != 1 || g.Rows[0].Hatched[0] != (FlowSpan{0, 1}) || g.Rows[0].Log[1] != 10 {
		t.Fatal("retention boundary hid stored data", g.Rows[0])
	}
	n.RetainedFromAt = testNow.Add(-time.Hour)
	n.Observed = &FlowObserved{FromAt: testNow.Add(-53 * time.Minute), LogBytesPerMin: []int64{20}}
	g = FlowGeometry(FlowView{Lead: &n, ObservedAt: testNow, WindowMinutes: 60, Gaps: []model.Gap{{From: testNow.Add(-55 * time.Minute), To: testNow.Add(-53 * time.Minute)}}}, 60, false)
	if g.Rows[0].Hatched[0] != (FlowSpan{5, 7}) || g.Rows[0].Log[7] != 20 {
		t.Fatal("gap boundary hid stored data", g.Rows[0])
	}
}
