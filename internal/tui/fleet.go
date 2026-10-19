package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/labstack/driga/internal/view"
)

func (m *app) localMachine() string {
	if m.snap.LocalMachineID != "" {
		return m.snap.LocalMachineID
	}
	return m.snap.Summary.MachineID
}
func (m *app) remote(id string) bool {
	return id != "" && m.localMachine() != "" && id != m.localMachine()
}
func (m *app) machineName(id string) string {
	for _, r := range m.snap.Machines.Rows {
		if r.MachineID == id {
			return view.MachineName(r.Machine)
		}
	}
	return id
}
func (m *app) machineState(id string) string {
	for _, r := range m.snap.Machines.Rows {
		if r.MachineID == id {
			return view.MachineState(r.Machine, m.localMachine(), m.opts.Now())
		}
	}
	return "local"
}
func (m *app) machineStale(id string) bool {
	s := m.machineState(id)
	return s != "live" && s != "local"
}
func (m *app) machineAge(id string) string {
	for _, r := range m.snap.Machines.Rows {
		if r.MachineID == id {
			seen := r.LastContactAt
			if seen.IsZero() {
				seen = r.LastSeen
			}
			if seen.IsZero() {
				return "unknown"
			}
			return max(time.Duration(0), m.opts.Now().Sub(seen)).Round(time.Second).String()
		}
	}
	return "unknown"
}
func (m *app) machineVisible(id string) bool {
	if m.machineScope == "" || m.machineScope == "all" || id == "" {
		return true
	}
	if m.machineScope == "local" {
		return !m.remote(id)
	}
	for _, r := range m.snap.Machines.Rows {
		if r.MachineID == id && (r.Hostname == m.machineScope || r.DeviceID != "" && strings.HasPrefix(strings.ToUpper(r.DeviceID), strings.ToUpper(m.machineScope))) {
			return true
		}
	}
	return id == m.machineScope || m.machineName(id) == m.machineScope
}
func (m *app) cycleMachine() {
	ids := []string{"all", "local"}
	for _, r := range m.snap.Machines.Rows {
		if m.remote(r.MachineID) {
			ids = append(ids, r.MachineID)
		}
	}
	at := 0
	for i, id := range ids {
		if id == m.machineScope || m.machineScope != "" && m.machineScope != "all" && m.machineScope != "local" && m.machineVisible(id) {
			at = i
		}
	}
	m.machineScope = ids[(at+1)%len(ids)]
	m.status = "machine: " + m.machineScope
}
func (m *app) fleetHeader() string {
	var peers []string
	for _, r := range m.snap.Machines.Rows {
		if m.remote(r.MachineID) {
			s := m.machineState(r.MachineID)
			if s == "stale" {
				s += " " + m.machineAge(r.MachineID)
			}
			peers = append(peers, m.machineName(r.MachineID)+" "+s)
		}
	}
	if len(peers) == 0 {
		return ""
	}
	return fmt.Sprintf(" +%d (%s)", len(peers), strings.Join(peers, ", "))
}

func (m *app) visibleSummary() view.SummaryView {
	s := m.snap.Summary
	if m.machineScope == "" || m.machineScope == "all" {
		return s
	}
	s.Agents, s.Working, s.Waiting, s.Stalled, s.Inbox, s.Orphans, s.Reclaim = 0, 0, 0, 0, 0, 0, 0
	var count func([]view.AgentRow)
	count = func(rows []view.AgentRow) {
		for _, r := range rows {
			if m.machineVisible(r.MachineID) {
				s.Agents++
				switch strings.TrimPrefix(r.Label, "~") {
				case "working":
					s.Working++
				case "waiting":
					s.Waiting++
				case "stalled?":
					s.Stalled++
				}
			}
			count(r.Children)
		}
	}
	count(m.snap.Agents.Rows)
	for _, r := range m.snap.Inbox.Rows {
		if m.machineVisible(r.MachineID) {
			s.Inbox++
		}
	}
	for _, r := range m.snap.Orphans.Rows {
		if m.machineVisible(r.MachineID) {
			s.Orphans++
			s.Reclaim += r.RSSTotal
		}
	}
	for _, r := range m.snap.Machine.Machines {
		if m.machineVisible(r.MachineID) {
			s.Sample = r.Sample
			break
		}
	}
	return s
}
