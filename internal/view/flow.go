package view

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/jsonwire"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

// FlowNode contains a session and only usage measurements actually retained.
type FlowNode struct {
	MachineID      string        `json:"machine_id"`
	ID             int64         `json:"id"`
	ExternalID     string        `json:"external_id"`
	Adapter        string        `json:"adapter"`
	Kind           string        `json:"kind"`
	Title          string        `json:"title"`
	Project        string        `json:"project"`
	Branch         string        `json:"branch"`
	Label          string        `json:"label"`
	Heuristic      bool          `json:"heuristic"`
	Age            time.Duration `json:"age_ms"`
	Elapsed        time.Duration `json:"elapsed_ms"`
	LastActivityAt time.Time     `json:"last_activity_at"`
	LastActivity   string        `json:"last_activity,omitempty"`
	LastTool       string        `json:"last_tool,omitempty"`
	Mem            *uint64       `json:"mem_bytes,omitempty"`
	Helpers        *int          `json:"helpers,omitempty"`
	Model          string        `json:"model,omitempty"`
	InputTokens    *int64        `json:"input_tokens,omitempty"`
	OutputTokens   *int64        `json:"output_tokens,omitempty"`
	CostUSD        *float64      `json:"cost_usd,omitempty"`
	Activity       []int         `json:"activity"`
}

// FlowView describes one family. Usage totals cover retained api_request events
// only (at most 200 stored events per session), never estimated lifetime usage.
type FlowView struct {
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

func (v FlowView) Workers(limit int, expanded bool, now time.Time) FlowWorkers {
	nodes := append([]FlowNode(nil), v.Children...)
	sortFlowWorkers(nodes)
	w := FlowWorkers{Nodes: []FlowNode{}}
	for _, n := range nodes {
		oldHosted := n.Kind == "hosted" && n.Label == "idle" && (n.LastActivityAt.IsZero() || now.Sub(n.LastActivityAt) > time.Hour)
		if !expanded && (oldHosted || len(w.Nodes) >= max(0, limit)) {
			w.Quiet++
		} else {
			w.Nodes = append(w.Nodes, n)
		}
	}
	return w
}

func sortFlowWorkers(nodes []FlowNode) {
	active := func(n FlowNode) bool { return n.Label == "working" || n.Label == "waiting" || n.Label == "stalled" }
	sort.SliceStable(nodes, func(i, j int) bool {
		if active(nodes[i]) != active(nodes[j]) {
			return active(nodes[i])
		}
		if nodes[i].LastActivityAt.Equal(nodes[j].LastActivityAt) {
			return nodes[i].ID < nodes[j].ID
		}
		return nodes[i].LastActivityAt.After(nodes[j].LastActivityAt)
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
		return title
	}
	if n.Project != "" {
		return FlowJoin(n.Project, n.Branch)
	}
	agent := strings.TrimSpace(n.Adapter)
	if agent == "" {
		agent = "agent"
	}
	return agent + " thread"
}

// Flow selects an explicit session's root, or the newest live interactive root.
// Machine and project scopes apply before selection; ancestry never crosses machines.
func Flow(r Reader, f Filter, sessionID int64, now time.Time) (FlowView, error) {
	v := FlowView{Children: []FlowNode{}, Tail: []TailEvent{}, UsageWindow: "retained api_request events"}
	var selectedReader Reader
	var selectedMachine model.Machine
	var all []model.Session
	var lead model.Session
	err := machineReaders(r, f.Scope, func(mr Reader, m model.Machine) error {
		ss, err := mr.Sessions(store.SessionFilter{IncludeEnded: true, RepoPath: f.Project})
		if err != nil {
			return err
		}
		for _, s := range ss {
			if sessionID != 0 {
				if s.ID != sessionID {
					continue
				}
			} else {
				if s.ParentID != nil || s.Hosted || strings.HasPrefix(s.Origin, "hosted") || s.PID == 0 || s.EndedAt != nil || s.Label == "dead" || !f.Since.IsZero() && s.LastActivityAt.Before(f.Since) {
					continue
				}
				if selectedReader != nil && !s.LastActivityAt.After(lead.LastActivityAt) {
					continue
				}
			}
			lead = s
			all = ss
			selectedReader = mr
			selectedMachine = m
			if sessionID != 0 {
				return nil
			}
		}
		return nil
	})
	if err != nil {
		return v, err
	}
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
	idx := derive.NewProcIndex(procs)
	repos, err := selectedReader.Repos()
	if err != nil {
		return v, err
	}
	items, err := selectedReader.Inbox(time.Time{}, false)
	if err != nil {
		return v, err
	}
	var leadEvents []model.Event
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
		n := FlowNode{MachineID: selectedReader.MachineID(), ID: s.ID, ExternalID: s.ExternalID, Adapter: s.Adapter, Kind: "worker", Title: s.Title, Project: projectName(s, repos), Branch: s.Branch, Label: s.Label, Heuristic: s.Heuristic, Age: sessionAge(s, now), LastActivityAt: s.LastActivityAt, Activity: make([]int, 10)}
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
		if s.ID == lead.ID {
			n.Kind = "lead"
			v.Lead = &n
			leadEvents = events
		} else {
			v.Children = append(v.Children, n)
		}
	}
	sortFlowWorkers(v.Children)
	if lead.Mixer != nil {
		v.Mixer = *lead.Mixer
		v.Mixer.Rows = append([]model.StateSignal(nil), lead.Mixer.Rows...)
	} else {
		in := derive.StateInput{Obs: lead.SessionObs, LastUserPrompt: lead.LastPromptAt}
		if reader, ok := selectedReader.(interface {
			IntakeState(int64) (store.IntakeState, error)
		}); ok {
			state, err := reader.IntakeState(lead.ID)
			if err != nil {
				return v, err
			}
			if !state.LastDecision.IsZero() {
				in.LastDecision = &state.LastDecision
			}
			if !state.LastAPIRequest.IsZero() {
				in.LastAPIRequest = &state.LastAPIRequest
			}
			in.LastAPIError = state.LastAPIError
		}
		if p, ok := idx.Get(lead.PID); ok && (lead.ProcStart.IsZero() || derive.SameStart(p.StartTime, lead.ProcStart)) {
			in.Proc = &p
		}
		for _, it := range items {
			if it.SessionID == lead.ID && it.Kind == "finished" {
				in.Unseen = true
			}
		}
		for _, e := range leadEvents {
			ts := e.TS
			if !strings.HasPrefix(e.Kind, "otlp:") {
				in.LastHook = e.Kind
				in.LastHookAt = e.TS
			}
			switch e.Kind {
			case "Stop":
				in.LastStop = &ts
			case "Notification", "PermissionRequest":
				in.LastNotification = &ts
			case "otlp:api_request":
				in.LastAPIRequest = &ts
			case "otlp:tool_result", "otlp:assistant_response", "otlp:user_prompt":
				in.LastToolResult = &ts
			}
		}
		v.Mixer = derive.LabelWithEvidence(in, now, derive.Thresholds{})
		v.Lead.Label = v.Mixer.Verdict
		v.Lead.Heuristic = v.Mixer.Heuristic
	}
	var lastTelemetry time.Time
	telemetryCount := 0
	for _, e := range leadEvents {
		if strings.HasPrefix(e.Kind, "otlp:") && !e.TS.After(now) {
			if e.TS.After(lastTelemetry) {
				lastTelemetry = e.TS
			}
			if !e.TS.Before(now.Add(-5 * time.Minute)) {
				telemetryCount++
			}
		}
	}
	v.EventsPerMinute = float64(telemetryCount) / 5
	for i := range v.Mixer.Rows {
		row := &v.Mixer.Rows[i]
		if !row.At.IsZero() {
			row.Age = max(time.Duration(0), now.Sub(row.At))
		}
		if row.Name == "telemetry" {
			row.Reading = fmt.Sprintf("%.1f / min", v.EventsPerMinute)
			if !lastTelemetry.IsZero() {
				row.At = lastTelemetry
				row.Age = max(time.Duration(0), now.Sub(lastTelemetry))
			}
		}
	}
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
