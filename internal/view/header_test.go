package view

import (
	"context"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"testing"
	"time"
)

func TestHeaderCountsSharesAttentionAndHygiene(t *testing.T) {
	inbox := InboxView{Rows: []InboxRow{{MachineID: "a", MachineFresh: MachineFresh{}, InboxItem: model.InboxItem{ID: "a", MachineID: "a", SessionID: 7, Kind: "finished"}}, {MachineID: "a", InboxItem: model.InboxItem{ID: "a2", MachineID: "a", SessionID: 7, Kind: "finished"}}, {MachineID: "b", InboxItem: model.InboxItem{ID: "b", MachineID: "b", SessionID: 7, Kind: "finished"}}, {InboxItem: model.InboxItem{ID: "x", Kind: "finished"}}, {InboxItem: model.InboxItem{ID: "y", Kind: "finished"}}, {InboxItem: model.InboxItem{Kind: "waiting"}}, {InboxItem: model.InboxItem{Kind: "low_disk"}}, {InboxItem: model.InboxItem{Kind: "worker_finished"}}}}
	agents := AgentsView{Rows: []AgentRow{{Label: "~stuck", Children: []AgentRow{{Label: "stuck"}}}}}
	orphans := OrphansView{Rows: []OrphanRow{{Orphan: model.Orphan{RSSTotal: 30}}, {Orphan: model.Orphan{RSSTotal: 40}}}}
	h := ProjectAttention(inbox, agents, "", nil).Header(orphans, SummaryView{Reclaim: 70, Sample: model.MachineSample{MemUsed: 50, MemTotal: 100}})
	if h.NeedYou != 2 || h.ToReview != 4 || h.Stuck != 2 || h.Leftover != 2 || h.LeftoverBytes != 70 || h.MemoryUsed != 50 || h.MemoryTotal != 100 {
		t.Fatal(h)
	}
}

func TestHeaderCountsWaitingSessions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		agents []AgentRow
		inbox  []InboxRow
		want   int
	}{
		{name: "waiting without inbox", agents: []AgentRow{{MachineID: "a", ID: 7, Label: "waiting"}}, want: 1},
		{name: "inferred child without inbox", agents: []AgentRow{{Label: "working", Children: []AgentRow{{MachineID: "a", ID: 7, Label: "~waiting", Heuristic: true}}}}, want: 1},
		{name: "waiting and prompt", agents: []AgentRow{{MachineID: "a", ID: 7, Label: "waiting"}}, inbox: []InboxRow{{MachineID: "a", InboxItem: model.InboxItem{SessionID: 7, Kind: "waiting"}}, {MachineID: "a", InboxItem: model.InboxItem{SessionID: 7, Kind: "approval"}}}, want: 1},
		{name: "same id on two machines", agents: []AgentRow{{MachineID: "a", ID: 7, Label: "waiting"}, {MachineID: "b", ID: 7, Label: "waiting"}}, want: 2},
		{name: "unbound inbox rows", inbox: []InboxRow{{InboxItem: model.InboxItem{Kind: "waiting"}}, {InboxItem: model.InboxItem{Kind: "low_disk"}}}, want: 2},
		{name: "duplicate inbox session", inbox: []InboxRow{{MachineID: "a", InboxItem: model.InboxItem{SessionID: 7, Kind: "waiting"}}, {MachineID: "a", InboxItem: model.InboxItem{SessionID: 7, Kind: "approval"}}}, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := ProjectAttention(InboxView{Rows: tc.inbox}, AgentsView{Rows: tc.agents}, "", nil).Header(OrphansView{}, SummaryView{})
			if h.NeedYou != tc.want {
				t.Fatalf("NeedYou = %d, want %d", h.NeedYou, tc.want)
			}
		})
	}
}

func TestProjectionMergesPromptsWithMachineSessionIdentity(t *testing.T) {
	ended := time.Now()
	inbox := InboxView{Rows: []InboxRow{
		{MachineID: "a", InboxItem: model.InboxItem{SessionID: 7, Kind: "approval"}},
		{MachineID: "a", InboxItem: model.InboxItem{SessionID: 7, Kind: "waiting"}},
		{MachineID: "a", Historical: true, InboxItem: model.InboxItem{SessionID: 8, Kind: "waiting"}},
		{MachineID: "a", InboxItem: model.InboxItem{SessionID: 9, Kind: "approval", EndedAt: &ended}},
		{MachineID: "a", InboxItem: model.InboxItem{SessionID: 10, Kind: "finished"}},
		{MachineID: "a", InboxItem: model.InboxItem{SessionID: 11, Kind: "worker_finished"}},
		{MachineID: "b", InboxItem: model.InboxItem{SessionID: 12, Kind: "waiting"}},
	}}
	agents := AgentsView{Rows: []AgentRow{{MachineID: "a", ID: 1, Label: "waiting", Children: []AgentRow{{MachineID: "a", ID: 2, Label: "~waiting"}}}, {MachineID: "b", ID: 3, Label: "waiting"}}}
	if got := ProjectAttention(inbox, agents, "", nil).Header(OrphansView{}, SummaryView{}).NeedYou; got != 5 {
		t.Fatalf("NeedYou = %d, want 5", got)
	}
	// Local waits and approval markers are already counted. Repeated prompts
	// for a new session count once, while each unbound prompt counts separately.
	var prompts []chat.Prompt
	for _, id := range []int64{1, 2, 7, 3, 3, 12, 0, 0} {
		prompts = append(prompts, chat.Prompt{SessionID: id})
	}
	if got := ProjectAttention(inbox, agents, "a", prompts).Header(OrphansView{}, SummaryView{}).NeedYou; got != 9 {
		t.Fatalf("merged NeedYou = %d, want 9", got)
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

func TestAttentionProjectionKeepsPromptsMemoryOnlyAndFlowConsistent(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	ended := now.Add(-time.Minute)
	data := BoardSnapshot{Nodes: []FlowNode{
		{MachineID: "a", ID: 7, Label: "working", StartedAt: now.Add(-time.Hour)},
		{MachineID: "b", ID: 7, Label: "working", StartedAt: now.Add(-time.Hour)},
		{MachineID: "a", ID: 8, Label: "finished", StartedAt: now.Add(-time.Hour), EndedAt: &ended},
	}}
	prompts := []chat.Prompt{{ID: "permission", SessionID: 7, Tool: "Bash", CreatedAt: now.Add(-2 * time.Minute)}, {ID: "duplicate", SessionID: 7, CreatedAt: now.Add(-time.Minute)}}
	a := ProjectAttention(InboxView{}, AgentsView{}, "a", prompts)
	b := a.Board(data, time.Hour, now)
	if a.Header(OrphansView{}, SummaryView{}).NeedYou != 1 || len(b.Needs) != 1 || b.Pulse["waiting"] != 1 || b.Needs[0].Node.MachineID != "a" || b.Needs[0].PromptID != "duplicate" {
		t.Fatalf("header/board/pulse disagree: %+v %+v", a, b)
	}
	if b.Needs[0].Wait == nil || !b.Needs[0].Wait.StartedAt.Equal(prompts[0].CreatedAt) {
		t.Fatal("merging prompts lost the oldest live wait")
	}
	if len(data.Nodes[0].Waits) != 0 || len(data.Nodes[1].Waits) != 0 {
		t.Fatal("prompt mutated the stored snapshot")
	}
	// Answering removes the overlay; no stale prompt can survive a new projection.
	a = ProjectAttention(InboxView{}, AgentsView{}, "a", nil)
	b = a.Board(data, time.Hour, now)
	if a.Header(OrphansView{}, SummaryView{}).NeedYou != 0 || len(b.Needs) != 0 || b.Pulse["waiting"] != 0 {
		t.Fatalf("answered prompt retained: %+v", b)
	}
	// Even a lagging prompt list cannot put an ended session back in Flow.
	b = ProjectAttention(InboxView{}, AgentsView{}, "a", []chat.Prompt{{ID: "ended", SessionID: 8}}).Board(data, time.Hour, now)
	if len(b.Needs) != 0 || b.Pulse["waiting"] != 0 {
		t.Fatalf("ended session regained a wait: %+v", b)
	}
}
