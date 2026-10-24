package view_test

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/labstack/driga/internal/boardtest"
	"github.com/labstack/driga/internal/flowtest"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

func TestBoardDeterministicSlots(t *testing.T) {
	s := boardtest.BoardFixture("busy", flowtest.Now)
	first := view.Board(s, time.Hour, flowtest.Now)
	positions := func(b view.BoardView) map[string][2]int {
		out := map[string][2]int{}
		for _, n := range b.Map.Nodes {
			out[n.Key] = [2]int{n.X, n.Y}
		}
		return out
	}
	for i := range s.Nodes {
		s.Nodes[i].Label = "idle"
		s.Nodes[i].Mixer.Verdict = "idle"
		s.Nodes[i].LastActivityAt = flowtest.Now.Add(time.Duration(i) * time.Second)
	}
	for i, j := 0, len(s.Nodes)-1; i < j; i, j = i+1, j-1 {
		s.Nodes[i], s.Nodes[j] = s.Nodes[j], s.Nodes[i]
	}
	next := view.Board(s, time.Hour, flowtest.Now.Add(time.Second))
	if !reflect.DeepEqual(positions(first), positions(next)) {
		t.Fatal("activity or input order moved slots")
	}
	if len(first.Needs) != first.Pulse["waiting"] {
		t.Fatal("needs/header count mismatch")
	}
	rows := 0
	for _, g := range first.Groups {
		rows += len(g.Rows)
	}
	if rows != len(s.Nodes) {
		t.Fatal("sessions silently hidden", rows, len(s.Nodes))
	}
}
func TestBoardFoldsWorkersAndKeepsCompleteTimeline(t *testing.T) {
	now := flowtest.Now
	parent := int64(1)
	s := view.BoardSnapshot{}
	for i := 0; i < 40; i++ {
		n := view.FlowNode{ID: int64(i + 1), MachineID: "local", Project: "project", Title: "Work", Label: "working", StartedAt: now.Add(time.Duration(i-40) * time.Minute)}
		if i > 0 {
			n.ParentID = &parent
		}
		s.Nodes = append(s.Nodes, n)
	}
	b := view.Board(s, time.Hour, now)
	if len(b.Map.Nodes) != 5 || len(b.Map.Badges) != 1 || b.Map.Badges[0].Text != "+35" || len(b.Groups[0].Rows) != 40 {
		t.Fatal("folding loses sessions", b.Map.Badges, len(b.Map.Nodes))
	}
	if len(b.Map.Edges) != 4 {
		t.Fatal("only stored spawn links may appear", b.Map.Edges)
	}
}
func TestBoardOverlapRequiresSameMachineAndFullPath(t *testing.T) {
	now := flowtest.Now
	s := view.BoardSnapshot{Files: map[string][]model.FileTouch{}, Remote: map[string]bool{"peer": true}}
	for i, machine := range []string{"local", "local", "peer", "local"} {
		n := view.FlowNode{ID: int64(i + 1), MachineID: machine, Project: "project", Label: "working"}
		s.Nodes = append(s.Nodes, n)
		path := "/project/file.go"
		if i == 3 {
			path = "/other/file.go"
		}
		s.Files[view.BoardKey(n)] = []model.FileTouch{{Path: path, TS: now}}
	}
	b := view.Board(s, time.Hour, now)
	overlaps := 0
	for _, e := range b.Map.Edges {
		if e.Kind == "overlap" {
			overlaps++
			if e.File != "file.go" {
				t.Fatal(e)
			}
			// The link stays in the region's left margin, clear of every node
			// and title: x never reaches past the nodes' left edge.
			var x0, y0, x1, y1, x2 int
			if _, err := fmt.Sscanf(e.Path, "M %d %d H %d V %d H %d", &x0, &y0, &x1, &y1, &x2); err != nil {
				t.Fatal(e.Path, err)
			}
			for _, n := range b.Map.Nodes {
				if n.Region == e.Region && max(x0, x1, x2) >= n.X-14 {
					t.Fatalf("overlap link %q crosses node %q at x=%d", e.Path, n.Key, n.X)
				}
			}
		}
	}
	if overlaps != 1 {
		t.Fatal("false overlaps", overlaps)
	}
	dashed := false
	for _, g := range b.Map.Regions {
		dashed = dashed || g.Dash != ""
	}
	if !dashed {
		t.Fatal("paired region missing")
	}
}
func TestBoardHonestyAndNeedsParity(t *testing.T) {
	now := flowtest.Now
	s := view.BoardSnapshot{}
	for i, adapter := range []string{"claude", "copilot", "codex", "gemini"} {
		n := view.FlowNode{ID: int64(i + 1), Adapter: adapter, Label: "waiting", StartedAt: now.Add(-time.Hour), Events: []model.Event{{TS: now.Add(-time.Minute), Kind: "otlp:tool_result"}, {TS: now.Add(-2 * time.Minute), Kind: "postToolUse"}}, Waits: []view.FlowWait{{StartedAt: now.Add(-time.Duration(i+1) * time.Minute)}}}
		s.Nodes = append(s.Nodes, n)
	}
	b := view.Board(s, time.Hour, now)
	if b.Pulse["waiting"] != len(b.Needs) || len(b.Needs) != 4 || b.Needs[0].Node.Adapter != "gemini" {
		t.Fatal("attention scope/order/parity", b.Needs)
	}
	for _, g := range b.Groups {
		for _, r := range g.Rows {
			tools := 0
			for _, c := range r.Slices {
				tools += c.Tools
			}
			if r.Node.Adapter == "codex" || r.Node.Adapter == "gemini" {
				if tools != 0 || view.BoardTools(r) != "" {
					t.Fatal("invented telemetry", r)
				}
			} else if tools != 1 {
				t.Fatal("lost telemetry", r.Node.Adapter, tools)
			}
		}
	}
	n := view.FlowNode{ID: 1, Label: "working", StartedAt: now.Add(-time.Hour)}
	unknown := view.Board(view.BoardSnapshot{Nodes: []view.FlowNode{n}}, time.Hour, now).Groups[0].Rows[0]
	if unknown.Slices[0].State != "unknown" || unknown.Slices[59].State != "working" {
		t.Fatal("current verdict painted backward")
	}
	for _, e := range b.Map.Edges {
		if e.Kind != "spawn" && e.Kind != "overlap" {
			t.Fatal("invented link", e.Kind)
		}
	}
}
func TestBoardEmptyQuietAndTitle(t *testing.T) {
	for _, kind := range []string{"empty", "quiet"} {
		b := view.Board(boardtest.BoardFixture(kind, flowtest.Now), time.Hour, flowtest.Now)
		if len(b.Needs) != 0 {
			t.Fatal(kind)
		}
		if kind == "quiet" && len(b.Map.Nodes) != 2 {
			t.Fatal("idle sessions hidden")
		}
	}
	n := view.FlowNode{Title: "Build · from Editor · from Editor", StartedFrom: "Editor"}
	if n.DisplayTitle() != "Build · from Editor" {
		t.Fatal(n.DisplayTitle())
	}
}
func BenchmarkBoard200(b *testing.B) {
	s := boardtest.BoardFixture("busy", flowtest.Now)
	for len(s.Nodes) < 200 {
		n := s.Nodes[len(s.Nodes)%70]
		n.ID += int64(len(s.Nodes) * 100)
		s.Nodes = append(s.Nodes, n)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		view.Board(s, time.Hour, flowtest.Now)
	}
}
func TestBoardLocalOnlyForThisMachine(t *testing.T) {
	b := view.Board(boardtest.BoardFixture("busy", time.Now()), time.Hour, time.Now())
	for _, g := range b.Groups {
		for _, r := range g.Rows {
			if r.Local == (r.Node.MachineID == "studio-mac") {
				t.Fatal("local flag wrong for", r.Node.MachineID)
			}
		}
	}
}
