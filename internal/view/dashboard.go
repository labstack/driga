package view

import (
	"fmt"
	attention "github.com/labstack/driga/internal/inbox"
	"sort"
	"strings"
	"time"
)

// Attention keeps the newest unacknowledged event and the session identity together.
// Inbox is nil for a waiting/stalled session with no unacknowledged event.
type Attention struct {
	MachineID                                                string
	ID                                                       int64
	State, Adapter, Title, Project, Machine, Summary, Branch string
	At                                                       time.Time
	Inbox                                                    *InboxRow
}

type sessionKey struct {
	machine string
	id      int64
}

// NeedsYou selects one item per machine/session, newest first. Current waiting
// and stalled sessions remain visible even when no inbox event was collected.
func NeedsYou(inbox []InboxRow, agents []AgentRow, details map[int64]AgentView) []Attention {
	items := map[sessionKey]Attention{}
	rows := map[sessionKey]AgentRow{}
	var walk func([]AgentRow)
	walk = func(agents []AgentRow) {
		for _, a := range agents {
			rows[sessionKey{a.MachineID, a.ID}] = a
			walk(a.Children)
		}
	}
	walk(agents)
	for _, i := range inbox {
		if i.Historical || i.AckedAt != nil || i.EndedAt != nil || i.Kind != "waiting" && i.Kind != "finished" && i.Kind != "worker_finished" {
			continue
		}
		key := sessionKey{i.MachineID, i.SessionID}
		if previous, ok := items[key]; ok && previous.Inbox != nil && !attention.Less(i.InboxItem, previous.Inbox.InboxItem) {
			continue
		}
		item := i
		items[key] = Attention{MachineID: i.MachineID, ID: i.SessionID, State: i.Kind, Project: i.Project, Machine: i.MachineName, Summary: i.Summary, At: i.TS, Inbox: &item, Adapter: i.Adapter, Title: i.Title, Branch: i.Branch}
	}
	for key, a := range rows {
		state := strings.TrimSuffix(strings.TrimPrefix(a.Label, "~"), "?")
		if _, ok := items[key]; ok || state != "waiting" && state != "stalled" {
			continue
		}
		at := a.LastActivityAt
		if at.IsZero() {
			at = details[a.ID].Session.LastActivityAt
		}
		items[key] = Attention{MachineID: a.MachineID, ID: a.ID, State: a.Label, At: at}
	}
	result := make([]Attention, 0, len(items))
	for key, item := range items {
		a := rows[key]
		d, hasDetail := details[item.ID]
		// Detail IDs are globally mapped by the store. Guard against a mismatched
		// machine in callers with synthetic or partially loaded snapshots.
		if d.MachineID != "" && d.MachineID != item.MachineID {
			d = AgentView{}
			hasDetail = false
		}
		if a.Branch != "" {
			item.Branch = a.Branch
		}
		if d.Session.Branch != "" {
			item.Branch = d.Session.Branch
		}
		if d.Session.Adapter != "" {
			item.Adapter = d.Session.Adapter
		}
		if item.Adapter == "" {
			item.Adapter = a.Adapter
		}
		if hasDetail {
			item.Title = d.Session.Title
		}
		if item.Title == "" && !hasDetail && !a.TitleFromPrompt {
			item.Title = a.Title
		}
		if item.Project == "" {
			item.Project = a.Project
		}
		if item.Title == "" {
			item.Title = item.Project
		}
		if item.Machine == "" {
			item.Machine = a.MachineName
		}
		label := d.Label
		if label == "" {
			label = a.Label
		}
		if state := strings.TrimSuffix(strings.TrimPrefix(label, "~"), "?"); state == "waiting" || state == "stalled" {
			item.State = label
		}
		if item.Summary == "" {
			item.Summary = item.Title
		}
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.Inbox != nil && b.Inbox != nil {
			return attention.Less(a.Inbox.InboxItem, b.Inbox.InboxItem)
		}
		if !a.At.Equal(b.At) {
			return a.At.After(b.At)
		}
		if a.MachineID != b.MachineID {
			return a.MachineID < b.MachineID
		}
		return a.ID < b.ID
	})
	return result
}

// WaitingHeader uses inbox generations, not inferred process states.
func WaitingHeader(rows []InboxRow, now time.Time) string {
	count := 0
	var oldest time.Duration
	for _, i := range rows {
		if attention.Active(i.InboxItem) && !i.Historical {
			count++
			oldest = max(oldest, attention.Waited(i.InboxItem, now))
		}
	}
	if count == 0 {
		return "0 waiting"
	}
	return fmt.Sprintf("%d waiting · oldest %s", count, oldest.Round(time.Second))
}

// AgentWaitingHeader counts the same visible family rows as Summary and Agents.
// Inbox timestamps supply duration only; an old generation cannot add a waiter.
func AgentWaitingHeader(rows []InboxRow, agents []AgentRow, now time.Time) string {
	waiting := map[sessionKey]time.Time{}
	var walk func([]AgentRow)
	walk = func(agents []AgentRow) {
		for _, a := range agents {
			if strings.TrimSuffix(strings.TrimPrefix(a.Label, "~"), "?") == "waiting" {
				waiting[sessionKey{a.MachineID, a.ID}] = a.LastActivityAt
			}
			walk(a.Children)
		}
	}
	walk(agents)
	var oldest time.Duration
	for _, i := range rows {
		key := sessionKey{i.MachineID, i.SessionID}
		if _, ok := waiting[key]; ok && attention.Active(i.InboxItem) && !i.Historical {
			at := i.WaitingSinceAt
			if at.IsZero() {
				at = i.TS
			}
			waiting[key] = at
		}
	}
	for _, at := range waiting {
		if !at.IsZero() {
			oldest = max(oldest, now.Sub(at))
		}
	}
	if len(waiting) == 0 {
		return "0 waiting"
	}
	return fmt.Sprintf("%d waiting · oldest %s", len(waiting), oldest.Round(time.Second))
}

// StateCount aggregates heuristic and confirmed labels in a fixed display order.
type StateCount struct {
	Label string
	Count int
}

func StateCounts(agents []AgentRow) []StateCount {
	counts := map[string]int{}
	for _, a := range agents {
		counts[strings.TrimSuffix(strings.TrimPrefix(a.Label, "~"), "?")]++
	}
	result := []StateCount{}
	for _, label := range []string{"working", "waiting", "stalled", "idle", "finished", "ended", "dead", "unknown"} {
		if counts[label] > 0 {
			result = append(result, StateCount{label, counts[label]})
		}
	}
	return result
}

// SortedProjects returns live projects first, then latest sourced changes.
// It copies the rows so rendering never mutates a shared snapshot.
func SortedProjects(rows []ProjectRow) []ProjectRow {
	result := append([]ProjectRow(nil), rows...)
	latest := func(p ProjectRow) time.Time {
		var at time.Time
		for _, part := range p.Status.Where {
			if part.At.After(at) {
				at = part.At
			}
		}
		return at
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if (a.Status.LiveAgents > 0) != (b.Status.LiveAgents > 0) {
			return a.Status.LiveAgents > 0
		}
		at, bt := latest(a), latest(b)
		if !at.Equal(bt) {
			return at.After(bt)
		}
		if a.Status.Repo.Name != b.Status.Repo.Name {
			return a.Status.Repo.Name < b.Status.Repo.Name
		}
		return a.MachineID < b.MachineID
	})
	return result
}

// ActivityHasSamples distinguishes an all-gap history from measured zero load.
func ActivityHasSamples(buckets []ActivityBucket) bool {
	for _, b := range buckets {
		if b.Sample != nil {
			return true
		}
	}
	return false
}

func ActivityHasEvents(buckets []ActivityBucket) bool {
	for _, b := range buckets {
		if b.Events > 0 {
			return true
		}
	}
	return false
}

const HistoryEmpty = "History builds while the collector runs"
const EventsEmpty = "Events need agent hooks: driga hooks install"
