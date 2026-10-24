package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"context"
	"encoding/json"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"github.com/labstack/driga/internal/boardtest"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/flowtest"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func boardApp(t *testing.T, kind string) *app {
	t.Helper()
	m := setup(t, fixture(), Options{NoColor: true, Now: func() time.Time { return flowtest.Now }}).(*app)
	m.snap.BoardData = boardtest.BoardFixture(kind, flowtest.Now)
	m.tab = flowTab
	m.width = 140
	m.height = 40
	m.loaded = true
	m.rebuild()
	return m
}
func TestBoardKeysViewportSelection(t *testing.T) {
	m := boardApp(t, "busy")
	original := m.boardSelected().Key
	for i := 0; i < 32; i++ {
		m.Update(tea.KeyPressMsg{Code: 'j'})
	}
	chosen := m.boardSelected().Key
	if chosen == original {
		t.Fatal("selection did not move")
	}
	text, _ := m.boardLayout()
	if !strings.Contains(text, "›") {
		t.Fatal("selection invisible")
	}
	for _, w := range []int{80, 100, 140, 200} {
		m.width = w
		for _, h := range []int{24, 40, 60} {
			m.height = h
			body, _ := m.boardLayout()
			if !strings.Contains(body, "›") {
				t.Fatal("selected row offscreen", w, h, body)
			}
			if lipgloss.Height(body) > m.bodyHeight() {
				t.Fatal("height overflow", w, h, lipgloss.Height(body), m.bodyHeight())
			}
			for _, line := range strings.Split(body, "\n") {
				if lipgloss.Width(line) > w {
					t.Fatal("width overflow", w, line)
				}
			}
		}
	}
	m.width = 140
	m.height = 40
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	for i := 0; i < 20; i++ {
		m.Update(tea.KeyPressMsg{Code: 'j'})
	}
	r := m.boardSelected()
	text, _ = m.boardLayout()
	if r == nil || !strings.Contains(text, r.Title) || !strings.Contains(text, "› ◆") {
		t.Fatal("selected wait hidden", text)
	}
	for _, tc := range []struct {
		key  rune
		mode string
	}{{'m', "Map"}, {'t', "Timeline"}, {'s', "Sessions"}} {
		m.Update(tea.KeyPressMsg{Code: tc.key})
		if m.flowViewMode != tc.mode {
			t.Fatal("view key", tc)
		}
	}

}
func TestBoardDoesNotReorderSelection(t *testing.T) {
	m := boardApp(t, "busy")
	m.boardCursor = 8
	selected := m.boardSelected().Key
	order := strings.Join(m.boardOrder, ",")
	for i := range m.snap.BoardData.Nodes {
		m.snap.BoardData.Nodes[i].LastActivityAt = flowtest.Now.Add(time.Duration(i) * time.Second)
	}
	m.syncBoard()
	next := strings.Join(m.boardOrder, ",")
	before := strings.Split(order, ",")
	after := strings.Split(next, ",")
	oldSet, newSet := map[string]bool{}, map[string]bool{}
	for _, k := range before {
		oldSet[k] = true
	}
	for _, k := range after {
		newSet[k] = true
	}
	commonBefore, commonAfter := []string{}, []string{}
	for _, k := range before {
		if newSet[k] {
			commonBefore = append(commonBefore, k)
		}
	}
	for _, k := range after {
		if oldSet[k] {
			commonAfter = append(commonAfter, k)
		}
	}
	if strings.Join(commonBefore, ",") != strings.Join(commonAfter, ",") || m.boardSelected().Key != selected {
		t.Fatal("existing rows or selection reordered")
	}

}
func TestBoardGoldenFrames(t *testing.T) {
	for _, kind := range []string{"busy", "quiet", "empty"} {
		for _, width := range []int{80, 100, 140, 200} {
			name := fmt.Sprintf("board-%s-%d", kind, width)
			t.Run(name, func(t *testing.T) {
				m := boardApp(t, kind)
				m.width = width
				m.height = 40
				got := m.flowView() + "\n"
				if strings.Contains(got, "\x1b") {
					t.Fatal("no-color emitted escapes")
				}
				path := filepath.Join("testdata", name+".golden")
				if os.Getenv("UPDATE_FLOW_GOLDEN") == "1" {
					if err := os.WriteFile(path, []byte(got), 0644); err != nil {
						t.Fatal(err)
					}
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
					t.Fatal("golden drift", path)
				}
				if dir := os.Getenv("FLOW_SHOTS_DIR"); dir != "" {
					if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(got), 0644); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestBoardThemesAndMotionKeepGeometry(t *testing.T) {
	defer func() { _ = view.SetTheme("system") }()
	for _, width := range []int{80, 100, 140, 200} {
		m := boardApp(t, "busy")
		m.width = width
		plain := m.flowView()
		for _, theme := range []string{"light", "dark"} {
			if err := view.SetTheme(theme); err != nil {
				t.Fatal(err)
			}
			m.opts.NoColor = false
			for _, off := range []bool{false, true} {
				m.opts.MotionOff = off
				got := m.flowView()
				if ansi.Strip(got) != plain {
					a, b := strings.Split(plain, "\n"), strings.Split(ansi.Strip(got), "\n")
					for i := range a {
						if i < len(b) && a[i] != b[i] {
							t.Fatalf("theme geometry %s %d line %d: %q => %q", theme, width, i, a[i], b[i])
						}
					}
					t.Fatal("theme or motion changed board geometry", theme, width)
				}
			}
		}
	}
}

func TestBoardPromptSafetyAndMachineReview(t *testing.T) {
	m := boardApp(t, "busy")
	now := flowtest.Now
	m.opts.Now = func() time.Time { return now }
	m.snap.LocalMachineID = "local"
	r := m.board.Needs[0]
	client := &promptClient{}
	m.opts.Chat = client
	m.prompts = []chat.Prompt{{ID: "board-prompt", SessionID: r.Node.ID}}
	m.Update(tea.KeyPressMsg{Code: 'y'})
	if client.id != "" {
		t.Fatal("unfocused prompt answered")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: 'y'})
	if client.id != "" {
		t.Fatal("prompt bypassed arming")
	}
	now = now.Add(time.Second)
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y'})
	execute(t, m, cmd)
	if client.id != "board-prompt" || !client.allow {
		t.Fatal("answer not dispatched")
	}
}
func TestBoardStopNeedsConfirmation(t *testing.T) {
	m := boardApp(t, "busy")
	var stopped []int64
	m.opts.SessionAction = func(_ context.Context, action string, id int64) error {
		if action == "kill" {
			stopped = append(stopped, id)
		}
		return nil
	}
	for m.boardSelected().State == "finished" || !m.boardSelected().Local {
		m.Update(tea.KeyPressMsg{Code: 'j'})
	}
	id := m.boardSelected().Node.ID
	m.Update(tea.KeyPressMsg{Code: 'K'})
	m.Update(tea.KeyPressMsg{Code: 'j'})
	if m.boardStop != 0 || len(stopped) != 0 {
		t.Fatal("another key must cancel a pending stop")
	}
	m.Update(tea.KeyPressMsg{Code: 'k'})
	// y and n answer prompts; they must never confirm a stop.
	for _, key := range []rune{'n', 'y'} {
		m.Update(tea.KeyPressMsg{Code: 'K'})
		if _, cmd := m.Update(tea.KeyPressMsg{Code: key}); cmd != nil {
			cmd()
		}
		if len(stopped) != 0 || m.boardStop != 0 {
			t.Fatalf("%c confirmed a stop", key)
		}
	}
	id = m.boardSelected().Node.ID
	m.Update(tea.KeyPressMsg{Code: 'K'})
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	execute(t, m, cmd)
	if len(stopped) != 1 || stopped[0] != id {
		t.Fatal("confirmed stop not dispatched", stopped, id)
	}
	for m.boardSelected().Local {
		m.Update(tea.KeyPressMsg{Code: 'j'})
	}
	m.Update(tea.KeyPressMsg{Code: 'K'})
	if m.boardStop != 0 {
		t.Fatal("remote session offered a local stop")
	}
}

func TestBoardMachineScopeCounts(t *testing.T) {
	m := boardApp(t, "busy")
	m.machineScope = "studio-mac"
	m.syncBoard()
	total := 0
	for _, v := range m.board.Pulse {
		total += v
	}
	if total != 11 {
		t.Fatal("machine scope counts", total)
	}
	for _, g := range m.board.Groups {
		for _, r := range g.Rows {
			if r.Node.MachineID != "studio-mac" {
				t.Fatal("machine scope leaked row")
			}
		}
	}
}

func TestFlowEnterOpensAndTerminalUsesStoredDestination(t *testing.T) {
	m := boardApp(t, "quiet")
	calls := 0
	m.opts.Exec = func([]string) error { calls++; return nil }
	m.snap.LocalMachineID = "local"
	m.snap.BoardData.Nodes[0].MachineID = "local"
	m.snap.BoardData.Nodes[0].Jump = fixtureLocalJump(t, model.InboxJump{Kind: "tmux", Server: "/tmp/fixture", Target: "dev:1.2", Command: "tmux attach -t 'dev:1.2'"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.detail || calls != 0 {
		t.Fatal("Enter executed a jump")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'J'})
	if cmd != nil {
		execute(t, m, cmd)
	}
	if calls == 0 {
		t.Fatal("Terminal did not use stored destination", m.status)
	}
}

func TestFlowFullPromptInspector(t *testing.T) {
	m := boardApp(t, "busy")
	m.snap.LocalMachineID = "local"
	r := m.board.Needs[0]
	command := strings.Repeat("long-command ", 120) + "end-marker"
	raw, _ := json.Marshal(map[string]string{"command": command})
	m.prompts = []chat.Prompt{{ID: "full", SessionID: r.Node.ID, Input: raw, Tool: "Bash"}}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.detail || !strings.Contains(m.detailText, "end-marker") || strings.Contains(m.detailText, "…") {
		t.Fatal("prompt command truncated")
	}
}

func TestBoardOpensOnTheOldestNeed(t *testing.T) {
	m := boardApp(t, "busy")
	if len(m.board.Needs) == 0 {
		t.Fatal("fixture has no needs")
	}
	if got, want := m.boardSelected().Key, m.board.Needs[0].Key; got != want {
		t.Fatalf("first selection = %s, want the oldest need %s", got, want)
	}
}
