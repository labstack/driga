package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/boardtest"
	"github.com/labstack/driga/internal/flowtest"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
)

func TestBoardRoutes(t *testing.T) {
	s, _, id, _ := fixture(t)
	for _, path := range []string{"/flow", fmt.Sprintf("/flow/%d", id), "/flow?view=timeline", "/flow?view=sessions"} {
		p, code, err := s.loadPage(httptest.NewRequest("GET", path, nil))
		if err != nil || code != 200 || !p.IsBoard {
			t.Fatal(path, code, err)
		}
		body := request(s, "GET", path).Body.String()
		for _, want := range []string{"data-flow-board", "fb-map", "role=\"tree\"", "Needs you ·", "morph:innerHTML"} {
			if !strings.Contains(body, want) {
				t.Error(path, "missing", want)
			}
		}
	}
	w := request(s, "GET", "/flow.json?view=board")
	var e struct{ Data view.BoardView }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &e) != nil || len(e.Data.Groups) == 0 {
		t.Fatal(w.Code, w.Body.String())
	}
}

// Set FLOW_SHOTS_DIR to export repeatable synthetic browser evidence. No server,
// collector, database mutation or agent CLI is involved in these fixtures.
func TestBoardFixturePages(t *testing.T) {
	dir := os.Getenv("FLOW_SHOTS_DIR")
	if dir == "" {
		return
	}
	s, _, _, _ := fixture(t)
	if err := os.MkdirAll(filepath.Join(dir, "fixtures"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"busy", "quiet", "empty"} {
		now := time.Now()
		b := view.Board(boardtest.BoardFixture(kind, now), time.Hour, now)
		prompts := map[string]string{}
		if len(b.Needs) > 0 {
			prompts[b.Needs[0].Key] = "synthetic-prompt"
		}
		b.WithPrompts(prompts)
		p := pageData{IsBoard: true, Board: b, Page: "/flow", Title: "Flow", Scope: "all", Nav: navigation, Snapshot: viewer.Snapshot{LocalMachineID: "local", Hostname: "Synthetic studio", Envelope: view.Envelope{ObservedAt: flowtest.Now, Mode: "snapshot"}}}
		p.AgentRows = boardFixtureRows(b)
		var out bytes.Buffer
		if err := s.templates.ExecuteTemplate(&out, "page", p); err != nil {
			t.Fatal(err)
		}
		raw := out.Bytes()
		if strings.Contains(string(raw), "ZgotmplZ") {
			t.Fatal("template error")
		}
		if err := os.WriteFile(filepath.Join(dir, "fixtures", kind+".html"), raw, 0644); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(b)
		if err := os.WriteFile(filepath.Join(dir, "fixtures", kind+".json"), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "fixtures", "theme.css"), []byte(view.ThemeCSS()), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestBoardClientLogic(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	js, err := assets.ReadFile("static/flow-board.js")
	if err != nil {
		t.Fatal(err)
	}
	test, err := os.ReadFile("testdata/flow-board.js")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "-e", string(js)+"\n"+string(test))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
}

func TestFlowDOM(t *testing.T) {
	runtime, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	js, err := assets.ReadFile("static/flow-board.js")
	if err != nil {
		t.Fatal(err)
	}
	harness, err := os.ReadFile("testdata/flow-board-dom.js")
	if err != nil {
		t.Fatal(err)
	}
	cmd := nodeScript(runtime, string(harness)+"\n"+string(js)+"\nrunFlowDOMTests().catch(e=>{console.error(e);process.exitCode=1;});")
	cmd.Env = []string{"PATH=" + t.TempDir(), "HOME=" + t.TempDir()}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
}

func boardFixtureRows(b view.BoardView) []view.AgentRow {
	rows := []view.AgentRow{}
	for _, g := range b.Groups {
		for _, r := range g.Rows {
			rows = append(rows, view.AgentRow{ID: r.Node.ID, MachineID: r.Node.MachineID, Adapter: r.Node.Adapter, Project: r.Node.Project, Branch: r.Node.Branch, Title: r.Title, Label: r.State, Age: max(time.Duration(0), b.ObservedAt.Sub(r.Node.StartedAt))})
		}
	}
	return rows
}
