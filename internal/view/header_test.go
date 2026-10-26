package view

import (
	"context"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"testing"
	"time"
)

func TestHeaderCountsSharesAttentionAndHygiene(t *testing.T) {
	inbox := InboxView{Rows: []InboxRow{{MachineID: "a", MachineFresh: MachineFresh{}, InboxItem: model.InboxItem{ID: "a", MachineID: "a", SessionID: 7, Kind: "finished"}}, {MachineID: "a", InboxItem: model.InboxItem{ID: "a2", MachineID: "a", SessionID: 7, Kind: "finished"}}, {MachineID: "b", InboxItem: model.InboxItem{ID: "b", MachineID: "b", SessionID: 7, Kind: "finished"}}, {InboxItem: model.InboxItem{ID: "x", Kind: "finished"}}, {InboxItem: model.InboxItem{ID: "y", Kind: "finished"}}, {InboxItem: model.InboxItem{Kind: "waiting"}}, {InboxItem: model.InboxItem{Kind: "low_disk"}}, {InboxItem: model.InboxItem{Kind: "worker_finished"}}}}
	agents := AgentsView{Rows: []AgentRow{{Label: "~stuck", Children: []AgentRow{{Label: "stuck"}}}}}
	orphans := OrphansView{Rows: []OrphanRow{{Orphan: model.Orphan{RSSTotal: 30}}, {Orphan: model.Orphan{RSSTotal: 40}}}}
	h := HeaderCounts(inbox, agents, orphans, SummaryView{Reclaim: 70, Sample: model.MachineSample{MemUsed: 50, MemTotal: 100}})
	if h.NeedYou != 2 || h.ToReview != 4 || h.Stuck != 2 || h.Leftover != 2 || h.LeftoverBytes != 70 || h.Reclaimable != 70 || h.MemoryUsed != 50 || h.MemoryTotal != 100 {
		t.Fatal(h)
	}
}

func TestMissingFleetSampleIsNil(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.UpsertMachine(model.Machine{Hostname: "no-sample"}) }); err != nil {
		t.Fatal(err)
	}
	v, err := Machines(st, time.Now())
	if err != nil || len(v.Rows) != 1 || v.Rows[0].Sample != nil {
		t.Fatalf("%+v %v", v, err)
	}
}
