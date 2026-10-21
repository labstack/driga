package view

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/inbox"
	"github.com/labstack/driga/internal/jsonwire"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/scrub"
	"github.com/labstack/driga/internal/store"
)

// FlowNode contains a session and only usage measurements actually retained.
type FlowNode struct {
	StartedFrom    string                  `json:"started_from,omitempty"`
	StartedAt      time.Time               `json:"started_at"`
	EndedAt        *time.Time              `json:"ended_at"`
	ParentID       *int64                  `json:"parent_id"`
	Mixer          model.StateMixer        `json:"mixer"`
	Events         []model.Event           `json:"events"`
	Waits          []FlowWait              `json:"waits"`
	Handoffs       []model.CompletionEvent `json:"handoffs"`
	RetainedFromAt time.Time               `json:"retained_from_at"`
	Jump           *model.InboxJump        `json:"jump,omitempty"`
	Reason         string                  `json:"reason,omitempty"`
	MachineID      string                  `json:"machine_id"`
	ID             int64                   `json:"id"`
	ExternalID     string                  `json:"external_id"`
	Adapter        string                  `json:"adapter"`
	Kind           string                  `json:"kind"`
	Title          string                  `json:"title"`
	Project        string                  `json:"project"`
	Branch         string                  `json:"branch"`
	Label          string                  `json:"label"`
	Heuristic      bool                    `json:"heuristic"`
	Age            time.Duration           `json:"age_ms"`
	Elapsed        time.Duration           `json:"elapsed_ms"`
	ElapsedText    string                  `json:"elapsed_text"`
	LastActivityAt time.Time               `json:"last_activity_at"`
	LastActivity   string                  `json:"last_activity,omitempty"`
	LastTool       string                  `json:"last_tool,omitempty"`
	Mem            *uint64                 `json:"mem_bytes,omitempty"`
	Helpers        *int                    `json:"helpers,omitempty"`
	Model          string                  `json:"model,omitempty"`
	InputTokens    *int64                  `json:"input_tokens,omitempty"`
	OutputTokens   *int64                  `json:"output_tokens,omitempty"`
	CostUSD        *float64                `json:"cost_usd,omitempty"`
	Activity       []int                   `json:"activity"`
	Observed       *FlowObserved           `json:"observed"`
}

// FlowObserved keeps collector measurements separate from agent-emitted events.
// Buckets include the current minute and the selected window, oldest first.
type FlowObserved struct {
	FromAt          time.Time `json:"from_at"`
	LogBytesPerMin  []int64   `json:"log_bytes_per_min"`
	CPUMSPerMin     []int64   `json:"cpu_ms_per_min"`
	LastGrowthAt    time.Time `json:"last_growth_at"`
	LastGrowthBytes int64     `json:"last_growth_bytes"`
}

func flowObserved(rows []store.ObservationMinute, now time.Time, minutes int) *FlowObserved {
	if len(rows) == 0 {
		return nil
	}
	o := &FlowObserved{LogBytesPerMin: make([]int64, minutes), CPUMSPerMin: make([]int64, minutes)}
	start := now.Truncate(time.Minute).Add(-time.Duration(minutes-1) * time.Minute)
	o.FromAt = start
	for _, row := range rows {
		if row.LastGrowthAt.After(o.LastGrowthAt) && !row.LastGrowthAt.After(now) {
			o.LastGrowthAt, o.LastGrowthBytes = row.LastGrowthAt, row.LastGrowthBytes
		}
		if row.Minute.Before(start) {
			continue
		}
		i := int(row.Minute.Sub(start) / time.Minute)
		if i >= 0 && i < minutes {
			o.LogBytesPerMin[i] = row.LogBytes
			o.CPUMSPerMin[i] = row.CPUMS
		}
	}
	return o
}

// FlowView describes one family. Usage totals cover retained api_request events
// only (at most 200 stored events per session), never estimated lifetime usage.
type FlowView struct {
	FromAt          time.Time        `json:"from_at"`
	ObservedAt      time.Time        `json:"observed_at"`
	WindowMinutes   int              `json:"window_minutes"`
	Gaps            []model.Gap      `json:"gaps"`
	Attention       FlowAttention    `json:"attention"`
	Lead            *FlowNode        `json:"lead"`
	Children        []FlowNode       `json:"children"`
	Mixer           model.StateMixer `json:"mixer"`
	EventsPerMinute float64          `json:"events_per_minute"`
	Tail            []TailEvent      `json:"tail"`
	UsageWindow     string           `json:"usage_window"`
}

func (v FlowView) MarshalJSON() ([]byte, error) { return jsonwire.Marshal(v) }
func (v FlowNode) MarshalJSON() ([]byte, error) { return jsonwire.Marshal(v) }

// FlowWorkers is a presentation window; the complete family stays in FlowView.
type FlowWorkers struct {
	Nodes []FlowNode
	Quiet int
}

// Workers folds only sessions with no retained evidence or open inbox wait.
// limit is retained for callers; active rows scroll rather than being called quiet.
func (v FlowView) Workers(limit int, expanded bool, now time.Time) FlowWorkers {
	nodes := append([]FlowNode(nil), v.Children...)
	sortFlowWorkers(nodes)
	w := FlowWorkers{Nodes: []FlowNode{}}
	for _, n := range nodes {
		quiet := len(n.Events) == 0 && len(n.Handoffs) == 0
		for _, wait := range n.Waits {
			if wait.EndedAt == nil {
				quiet = false
			}
		}
		if n.Observed != nil {
			for i, x := range n.Observed.LogBytesPerMin {
				if x > 0 || i < len(n.Observed.CPUMSPerMin) && n.Observed.CPUMSPerMin[i] > 0 {
					quiet = false
					break
				}
			}
		}
		if quiet && !expanded {
			w.Quiet++
		} else {
			w.Nodes = append(w.Nodes, n)
		}
	}
	return w
}
func sortFlowWorkers(nodes []FlowNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].StartedAt.Equal(nodes[j].StartedAt) {
			return nodes[i].ID < nodes[j].ID
		}
		return nodes[i].StartedAt.Before(nodes[j].StartedAt)
	})
}

// FlowJoin omits empty fields before adding separators.
func FlowJoin(parts ...string) string {
	kept := []string{}
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " · ")
}

func (n FlowNode) DisplayTitle() string {
	if title := strings.TrimSpace(n.Title); title != "" {
		return title + FromSuffix(n.StartedFrom)
	}
	if n.Project != "" {
		return FlowJoin(n.Project, n.Branch) + FromSuffix(n.StartedFrom)
	}
	agent := strings.TrimSpace(n.Adapter)
	if agent == "" {
		agent = "agent"
	}
	return agent + " thread" + FromSuffix(n.StartedFrom)
}

func flowAttentionTitle(s model.Session, project, capture string) string {
	hint := ""
	if capture != "none" {
		hint = scrub.Snippet(s.Title)
	}
	if hint == "" {
		hint = scrub.Snippet(s.Branch)
	}
	if hint == "" {
		id := []rune(strings.TrimSpace(s.ExternalID))
		if len(id) > 0 {
			hint = "#" + string(id[max(0, len(id)-6):])
		} else {
			hint = fmt.Sprintf("#%d", s.ID)
		}
	}
	return FlowJoin(project, scrub.Truncate(hint, 40))
}

// defaultFlowLead is the no-id selection policy for every flow surface.
// Families come first, then attention state, activity and a stable identity tie.
func defaultFlowLead(sessions []model.Session, since time.Time) *model.Session {
	type key struct {
		machine string
		id      int64
	}
	families := map[key]bool{}
	for _, s := range sessions {
		if s.ParentID != nil {
			families[key{s.MachineID, *s.ParentID}] = true
		}
	}
	attention := func(s model.Session) int {
		switch s.Label {
		case "waiting":
			return 3
		case "stalled":
			return 2
		case "working":
			return 1
		}
		return 0
	}
	var best *model.Session
	for i := range sessions {
		s := &sessions[i]
		// Empty origin is a legacy interactive session. Known headless and
		// hosted origins are never default leads, but remain explicitly openable.
		if s.ParentID != nil || s.Hosted || s.Origin != "" && !strings.HasPrefix(s.Origin, "interactive") || s.PID == 0 || s.EndedAt != nil || s.Label == "dead" || !since.IsZero() && s.LastActivityAt.Before(since) {
			continue
		}
		if best != nil {
			family, bestFamily := families[key{s.MachineID, s.ID}], families[key{best.MachineID, best.ID}]
			if family != bestFamily {
				if !family {
					continue
				}
			} else if attention(*s) != attention(*best) {
				if attention(*s) < attention(*best) {
					continue
				}
			} else if !s.LastActivityAt.Equal(best.LastActivityAt) {
				if s.LastActivityAt.Before(best.LastActivityAt) {
					continue
				}
			} else if s.MachineID > best.MachineID || s.MachineID == best.MachineID && s.ID >= best.ID {
				continue
			}
		}
		best = s
	}
	return best
}

// Flow selects an explicit session's root, or the shared default interactive lead.
// Machine and project scopes apply before selection; ancestry never crosses machines.
func Flow(r Reader, f Filter, sessionID int64, now time.Time) (FlowView, error) {
	minutes := f.FlowMinutes
	if minutes == 0 {
		minutes = 60
	}
	if minutes != 15 && minutes != 60 && minutes != 240 {
		return FlowView{}, fmt.Errorf("flow minutes must be 15, 60 or 240")
	}
	v := FlowView{ObservedAt: now, FromAt: now.Add(-time.Duration(minutes) * time.Minute), WindowMinutes: minutes, Gaps: []model.Gap{}, Attention: FlowAttention{Waits: []InboxRow{}}, Children: []FlowNode{}, Tail: []TailEvent{}, UsageWindow: "retained api_request events"}
	var selectedReader Reader
	var selectedMachine model.Machine
	var all []model.Session
	var lead model.Session
	type machineFlow struct {
		reader   Reader
		machine  model.Machine
		sessions []model.Session
	}
	byMachine := map[string]machineFlow{}
	var defaults []model.Session
	err := machineReaders(r, f.Scope, func(mr Reader, m model.Machine) error {
		// Attention is machine-wide, independent of selected project/family.
		ssAll, err := mr.Sessions(store.SessionFilter{IncludeEnded: true})
		if err != nil {
			return err
		}
		items, err := mr.Inbox(time.Time{}, true)
		if err != nil {
			return err
		}
		repos, err := mr.Repos()
		if err != nil {
			return err
		}
		sessions := map[int64]model.Session{}
		for _, s := range ssAll {
			sessions[s.ID] = s
			if s.Label == "working" && s.EndedAt == nil {
				v.Attention.Working++
			}
		}
		for _, item := range items {
			if inbox.Active(item) && !item.TS.After(now) {
				item = inbox.Policy(item, f.CaptureText)
				s := sessions[item.SessionID]
				project := projectName(s, repos)
				v.Attention.Waits = append(v.Attention.Waits, InboxRow{InboxItem: item, MachineID: mr.MachineID(), Adapter: s.Adapter, Title: flowAttentionTitle(s, project, f.CaptureText), Project: project, Branch: s.Branch, Age: max(0, now.Sub(item.TS)), Waited: inbox.Waited(item, now)})
			}
		}
		ss, err := mr.Sessions(store.SessionFilter{IncludeEnded: true, RepoPath: f.Project})
		if err != nil {
			return err
		}
		if sessionID == 0 {
			byMachine[mr.MachineID()] = machineFlow{mr, m, ss}
			defaults = append(defaults, ss...)
			return nil
		}
		for _, s := range ss {
			if s.ID != sessionID {
				continue
			}
			lead = s
			all = ss
			selectedReader = mr
			selectedMachine = m
			return nil
		}
		return nil
	})
	if err != nil {
		return v, err
	}
	if sessionID == 0 {
		if chosen := defaultFlowLead(defaults, f.Since); chosen != nil {
			lead = *chosen
			selected := byMachine[lead.MachineID]
			selectedReader, selectedMachine, all = selected.reader, selected.machine, selected.sessions
		}
	}
	sort.SliceStable(v.Attention.Waits, func(i, j int) bool {
		a, b := v.Attention.Waits[i], v.Attention.Waits[j]
		if a.Waited != b.Waited {
			return a.Waited > b.Waited
		}
		return inbox.Less(a.InboxItem, b.InboxItem)
	})
	if selectedReader == nil {
		if sessionID != 0 {
			return v, store.ErrNotFound
		}
		return v, nil
	}
	byID := map[int64]model.Session{}
	for _, s := range all {
		byID[s.ID] = s
	}
	seen := map[int64]bool{}
	for lead.ParentID != nil && !seen[lead.ID] {
		seen[lead.ID] = true
		p, ok := byID[*lead.ParentID]
		if !ok {
			break
		}
		lead = p
	}
	family := map[int64]bool{lead.ID: true}
	for changed := true; changed; {
		changed = false
		for _, s := range all {
			if s.ParentID != nil && family[*s.ParentID] && !family[s.ID] {
				family[s.ID] = true
				changed = true
			}
		}
	}
	procs, err := selectedReader.Procs()
	if err != nil {
		return v, err
	}
	idx := derive.NewProcIndexForOS(procs, selectedMachine.OS)
	repos, err := selectedReader.Repos()
	if err != nil {
		return v, err
	}
	var leadEvents []model.Event
	waits, err := selectedReader.Inbox(time.Time{}, true)
	if err != nil {
		return v, err
	}
	reasons := map[int64]string{}
	for _, i := range waits {
		if inbox.Active(i) {
			reasons[i.SessionID] = i.Reason
		}
	}
	for _, s := range all {
		if !family[s.ID] {
			continue
		}
		if s.ID != lead.ID && s.EndedAt != nil && s.EndedAt.Before(now.Add(-24*time.Hour)) {
			continue
		}
		events, err := selectedReader.Events(s.Adapter, s.ExternalID, time.Time{})
		if err != nil {
			return v, err
		}
		n := FlowNode{StartedFrom: s.StartedFrom, MachineID: selectedReader.MachineID(), ID: s.ID, ExternalID: s.ExternalID, Adapter: s.Adapter, Kind: "worker", Title: s.Title, Project: projectName(s, repos), Branch: s.Branch, Label: s.Label, Heuristic: s.Heuristic, Age: sessionAge(s, now), LastActivityAt: s.LastActivityAt, Activity: make([]int, 10)}
		n.StartedAt, n.EndedAt, n.ParentID, n.Mixer = s.StartedAt, s.EndedAt, s.ParentID, derive.SessionMixer(s)
		n.Events = []model.Event{}
		n.Waits = []FlowWait{}
		n.Handoffs = []model.CompletionEvent{}
		n.Jump = inbox.ResolveJump(s, "", nil)
		n.RetainedFromAt = time.Time{}
		for _, e := range events {
			if !e.TS.After(now) && (n.RetainedFromAt.IsZero() || e.TS.Before(n.RetainedFromAt)) {
				n.RetainedFromAt = e.TS
			}
		}
		var rows []store.ObservationMinute
		if selectedReader.MachineID() == r.MachineID() {
			var e error
			rows, e = selectedReader.ObservationMinutes(s.ID, now)
			if e != nil {
				return v, e
			}
		}
		if len(rows) > 0 {
			oldest := rows[0].Minute
			if n.RetainedFromAt.IsZero() || oldest.After(n.RetainedFromAt) {
				n.RetainedFromAt = oldest
			}
		}
		for _, item := range waits {
			if item.Kind != "waiting" || item.SessionID != s.ID {
				continue
			}
			item = inbox.Policy(item, f.CaptureText)
			w := flowWait(item)
			if w.StartedAt.After(now) || w.EndedAt != nil && w.EndedAt.Before(v.FromAt) {
				continue
			}
			n.Waits = append(n.Waits, w)
			if item.Jump != nil {
				n.Jump = item.Jump
			}
		}
		if s.Adapter == "codex" || s.Adapter == "gemini" {
			hs, e := selectedReader.SessionHandoffs(s.ID, v.FromAt, now)
			if e != nil {
				return v, e
			}
			for _, h := range hs {
				if !h.TS.Before(v.FromAt) && !h.TS.After(now) {
					n.Handoffs = append(n.Handoffs, h)
				}
			}
		}
		n.Reason = reasons[s.ID]
		n.Observed = flowObserved(rows, now, minutes)
		if strings.TrimSpace(n.Title) == "" {
			// LastPrompt has already passed the collector's capture policy.
			n.Title = strings.TrimSpace(s.LastPrompt)
		}
		n.Title = n.DisplayTitle()
		if s.Hosted || strings.HasPrefix(s.Origin, "hosted") {
			n.Kind = "hosted"
		}
		n.Elapsed = n.Age
		if s.EndedAt != nil && !s.StartedAt.IsZero() {
			n.Elapsed = max(time.Duration(0), s.EndedAt.Sub(s.StartedAt))
		}
		n.ElapsedText = SessionDuration(n.Elapsed)
		tree := procTree(s, idx)
		if len(tree) > 0 {
			var rss uint64
			helpers := 0
			for _, p := range tree {
				rss += p.RSS
				if p.PID != s.PID && p.Role == "helper" {
					helpers++
				}
			}
			n.Mem = &rss
			n.Helpers = &helpers
		}
		start := now.Truncate(time.Minute).Add(-9 * time.Minute)
		for _, e := range events {
			if e.TS.After(now) {
				continue
			}
			if !e.TS.Before(start) {
				bucket := int(e.TS.Sub(start) / time.Minute)
				if bucket < 10 {
					n.Activity[bucket]++
				}
			}
			if !e.TS.Before(v.FromAt) {
				n.Events = append(n.Events, e)
			}
			n.LastActivity = e.Kind
			title := n.Title
			if n.Project != "" {
				title = n.Project + " · " + title
			}
			v.Tail = append(v.Tail, tailEvent(e, selectedReader.MachineID(), MachineName(selectedMachine), title))
			if e.Metadata != nil {
				m := e.Metadata
				if m.Tool != "" {
					n.LastTool = m.Tool
				}
				if m.Model != "" {
					n.Model = m.Model
				}
				if e.Kind == "otlp:api_request" {
					if err := sumInt(&n.InputTokens, m.InputTokens); err != nil {
						return v, err
					}
					if err := sumInt(&n.OutputTokens, m.OutputTokens); err != nil {
						return v, err
					}
					if m.CostUSD != nil {
						if n.CostUSD == nil {
							n.CostUSD = new(float64)
						}
						if *m.CostUSD > math.MaxFloat64-*n.CostUSD {
							return v, fmt.Errorf("retained cost total overflows")
						}
						*n.CostUSD += *m.CostUSD
					}
				}
			} else if e.Kind == "otlp:tool_result" {
				n.LastTool = e.Summary
			}
		}
		if len(n.Events) > 100 {
			n.Events = n.Events[len(n.Events)-100:]
		}
		if s.ID == lead.ID {
			n.Kind = "lead"
			v.Lead = &n
			leadEvents = events
		} else {
			v.Children = append(v.Children, n)
		}
	}
	sortFlowWorkers(v.Children)
	v.Gaps, err = selectedReader.Gaps(v.FromAt)
	if err != nil {
		return v, err
	}
	v.Gaps = nonNil(v.Gaps)
	v.Mixer = derive.SessionMixer(lead)
	telemetryCount := 0
	for _, e := range leadEvents {
		if strings.HasPrefix(e.Kind, "otlp:") && !e.TS.After(now) {
			if !e.TS.Before(now.Add(-5 * time.Minute)) {
				telemetryCount++
			}
		}
	}
	v.EventsPerMinute = float64(telemetryCount) / 5
	sort.SliceStable(v.Tail, func(i, j int) bool {
		if v.Tail[i].TS.Equal(v.Tail[j].TS) {
			return v.Tail[i].Key < v.Tail[j].Key
		}
		return v.Tail[i].TS.Before(v.Tail[j].TS)
	})
	if len(v.Tail) > 200 {
		v.Tail = v.Tail[len(v.Tail)-200:]
	}

	return v, nil
}
func sumInt(dst **int64, src *int64) error {
	if src != nil {
		if *dst == nil {
			*dst = new(int64)
		}
		if *src > math.MaxInt64-**dst {
			return fmt.Errorf("retained token total overflows")
		}
		**dst += *src
	}
	return nil
}

// FlowSpark renders ten event-count buckets, with blank cells for no events.
func FlowSpark(buckets []int) string {
	peak := 0
	for _, n := range buckets {
		peak = max(peak, n)
	}
	var b strings.Builder
	blocks := []rune("▁▂▃▄▅▆▇█")
	for _, n := range buckets {
		if n == 0 || peak == 0 {
			b.WriteRune(' ')
		} else {
			b.WriteRune(blocks[min(7, (n*8-1)/peak)])
		}
	}
	return b.String()
}
