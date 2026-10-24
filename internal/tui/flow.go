package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

const flowTab = 7 // Internal pane identity; the public tab number is 0.
type flowHit struct{ x, y, width, height, index int }

func (m *app) flowScope() store.Scope {
	scope := store.Scope{}
	if m.machineScope != "" && m.machineScope != "all" {
		id := m.machineScope
		if id == "local" {
			id = m.localMachine()
		}
		scope.Machines = []string{id}
	}
	return scope
}

func (m *app) flowSelected() *view.FlowNode {
	if r := m.boardSelected(); r != nil {
		return &r.Node
	}
	return nil
}
func (m *app) jumpFlow() tea.Cmd {
	n := m.flowSelected()
	if n == nil {
		return nil
	}
	if n.Jump == nil {
		m.status = "No stored jump destination"
		return nil
	}
	return m.jumpInbox(tabRow{machine: n.MachineID, inbox: &model.InboxItem{Jump: n.Jump}})
}
func (m *app) flowLayout() (string, []flowHit) { return m.boardLayout() }
func (m *app) flowView() string                { text, _ := m.boardLayout(); return text }
func (m *app) flowWorkerHits() []flowHit       { _, hits := m.boardLayout(); return hits }
