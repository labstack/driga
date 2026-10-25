package web

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
)

// Each button's request reaches its action only for what the TUI would act
// on: local rows, verified idle trees, and a confirmation for every change.
func TestActionArgs(t *testing.T) {
	snap := viewer.Snapshot{LocalMachineID: "here"}
	snap.BoardData.Nodes = []view.FlowNode{{ID: 7, MachineID: "here"}, {ID: 8, MachineID: "peer"}}
	snap.Orphans.Rows = []view.OrphanRow{
		{MachineID: "here", Orphan: model.Orphan{ID: "ok", Verified: true}},
		{MachineID: "here", Orphan: model.Orphan{ID: "young", Verified: true, Young: true}},
		{MachineID: "here", Orphan: model.Orphan{ID: "unverified"}},
		{MachineID: "peer", Orphan: model.Orphan{ID: "remote", Verified: true}},
	}
	snap.Projects.Rows = []view.ProjectRow{{MachineID: "here", Status: view.ProjectStatus{Repo: model.Repo{Path: "/repo"}}}}
	for _, tc := range []struct {
		name string
		in   actionRequest
		want string // "" refuses
	}{
		{"jump", actionRequest{Action: "jump", SessionID: 7, Machine: "here"}, `{"session_id":7}`},
		{"jump remote", actionRequest{Action: "jump", SessionID: 8, Machine: "peer"}, ""},
		{"stop unconfirmed", actionRequest{Action: "kill", SessionID: 7, Machine: "here"}, ""},
		{"stop", actionRequest{Action: "kill", SessionID: 7, Machine: "here", Confirmed: true}, `{"force":false,"session_id":7}`},
		{"stop claims another machine", actionRequest{Action: "kill", SessionID: 7, Machine: "peer", Confirmed: true}, ""},
		{"kill tree", actionRequest{Action: "kill", ID: "ok", Machine: "here", Force: true, Confirmed: true}, `{"force":true,"id":"ok"}`},
		{"kill young tree", actionRequest{Action: "kill", ID: "young", Machine: "here", Confirmed: true}, ""},
		{"kill unverified tree", actionRequest{Action: "kill", ID: "unverified", Machine: "here", Confirmed: true}, ""},
		{"kill remote tree", actionRequest{Action: "kill", ID: "remote", Machine: "here", Confirmed: true}, ""},
		{"cleanup", actionRequest{Action: "cleanup", ID: "wt", Machine: "here", Confirmed: true}, `{"id":"wt"}`},
		{"cleanup unconfirmed", actionRequest{Action: "cleanup", ID: "wt", Machine: "here"}, ""},
		{"ack one remote", actionRequest{Action: "ack_inbox", ID: "w1", Machine: "peer"}, `{"all":false,"id":"w1"}`},
		{"ack all", actionRequest{Action: "ack_inbox", All: true, Confirmed: true}, `{"all":true,"id":""}`},
		{"ack all unconfirmed", actionRequest{Action: "ack_inbox", All: true}, ""},
		{"ack nothing", actionRequest{Action: "ack_inbox", Confirmed: true}, ""},
		{"summarize", actionRequest{Action: "summarize", Project: "/repo", Machine: "here"}, `{"project":"/repo"}`},
		{"summarize unknown", actionRequest{Action: "summarize", Project: "/elsewhere", Machine: "here"}, ""},
		{"unknown", actionRequest{Action: "rm", Confirmed: true}, ""},
	} {
		got, err := actionArgs(tc.in, snap)
		if tc.want == "" {
			if err == nil {
				t.Errorf("%s: allowed with %s", tc.name, got)
			}
			continue
		}
		if err != nil || string(got) != tc.want {
			t.Errorf("%s: %s, %v; want %s", tc.name, got, err, tc.want)
		}
	}
}

func TestAppActionRuns(t *testing.T) {
	s, st, id, _ := fixture(t)
	if w := postChat(s, "/app/action", `{"action":"jump","session_id":1}`); w.Code != 503 {
		t.Fatalf("without actions: %d", w.Code)
	}
	var name string
	var args json.RawMessage
	s.opts.Action = func(_ context.Context, n string, a json.RawMessage) error { name, args = n, a; return nil }
	body := fmt.Sprintf(`{"action":"kill","session_id":%d,"machine":%q,"confirmed":true}`, id, st.MachineID())
	if w := postChat(s, "/app/action", body); w.Code != 200 || name != "kill" || string(args) != fmt.Sprintf(`{"force":false,"session_id":%d}`, id) {
		t.Fatal(w.Code, w.Body.String(), name, string(args))
	}
	s.opts.Action = func(context.Context, string, json.RawMessage) error {
		return fmt.Errorf("session process changed or exited")
	}
	if w := postChat(s, "/app/action", body); w.Code != 400 || w.Body.String() != "session process changed or exited\n" {
		t.Fatal(w.Code, w.Body.String())
	}
}

// The app's pages carry the buttons; without actions they keep pointing at
// the CLI.
func TestActionButtons(t *testing.T) {
	s, _, _, project := fixture(t)
	pages := map[string]string{
		"/inbox":                          `data-action="ack_inbox" data-id="waiting"`,
		"/machines?reclaim=processes":     "Unverified: run driga orphans kill 42:1",
		fmt.Sprint("/projects/", project): `data-action="summarize" data-project="/fixture"`,
	}
	for path, want := range pages {
		if body := request(s, "GET", path).Body.String(); strings.Contains(body, want) {
			t.Errorf("%s offers %q without actions", path, want)
		}
	}
	s.opts.Action = func(context.Context, string, json.RawMessage) error { return nil }
	for path, want := range pages {
		if body := request(s, "GET", path).Body.String(); !strings.Contains(body, want) {
			t.Errorf("%s lacks %q", path, want)
		}
	}
}
