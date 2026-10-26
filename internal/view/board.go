package view

import (
	"fmt"
	"github.com/labstack/driga/internal/model"
	"sort"
	"strings"
	"time"
)

// BoardSnapshot is the shared overview evidence, loaded in bulk, never per row.
// Nodes carry raw titles. Events and waits retain their stored timestamps.
type BoardSnapshot struct {
	Files  map[string][]model.FileTouch `json:"files,omitempty"`
	Remote map[string]bool              `json:"remote,omitempty"`
	Nodes  []FlowNode                   `json:"nodes"`
	Load   []int                        `json:"load"`
	Memory string                       `json:"memory"`
	// History is the top-level sessions that ended in the last two weeks,
	// newest first, for finding and resuming past work.
	History []HistoryRow `json:"history,omitempty"`
}

// HistoryRow is one ended session and how to resume it, when its agent can.
type HistoryRow struct {
	MachineID string    `json:"machine_id"`
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Project   string    `json:"project"`
	Branch    string    `json:"branch"`
	Adapter   string    `json:"adapter"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	Resume    string    `json:"resume,omitempty"`
}

// HistoryWindow and HistoryLimit bound BoardSnapshot.History.
const (
	HistoryWindow = 14 * 24 * time.Hour
	HistoryLimit  = 500
)

type BoardSlice struct {
	At       time.Time `json:"at"`
	State    string    `json:"state"`
	Tools    int       `json:"tools"`
	Events   int       `json:"events"`
	Activity int64     `json:"activity"`
}
type BoardRow struct {
	Command    string            `json:"command,omitempty"`
	Depth      int               `json:"depth"`
	Parent     string            `json:"parent"`
	Root       string            `json:"root"`
	Branch     string            `json:"branch"`
	Age        string            `json:"age"`
	Glyph      string            `json:"glyph"`
	Local      bool              `json:"local"` // on this machine, so local actions apply
	Files      []model.FileTouch `json:"files,omitempty"`
	Overlaps   []string          `json:"overlaps,omitempty"`
	Quiet      bool              `json:"quiet"`
	Key        string            `json:"key"`
	Node       FlowNode          `json:"node"`
	Title      string            `json:"title"`
	State      string            `json:"state"`
	Cwd        string            `json:"cwd"`
	Slices     []BoardSlice      `json:"slices"`
	Workers    []string          `json:"workers"`
	Wait       *FlowWait         `json:"wait,omitempty"`
	PromptID   string            `json:"prompt_id,omitempty"`
	WaitReason string            `json:"wait_reason"`
	Cost       *float64          `json:"cost_usd,omitempty"`
}
type BoardGroup struct {
	Key       string     `json:"key"`
	Name      string     `json:"name"`
	Branches  []string   `json:"branches"`
	Worktrees int        `json:"worktrees"`
	Sessions  int        `json:"sessions"`
	Cost      *float64   `json:"cost_usd,omitempty"`
	Spark     []int      `json:"spark"`
	Rows      []BoardRow `json:"rows"`
}
type BoardView struct {
	Map           BoardMap               `json:"map"`
	Spark         []int                  `json:"spark"`
	Machines      map[string]*BoardGroup `json:"machines"`
	ObservedAt    time.Time              `json:"observed_at"`
	FromAt        time.Time              `json:"from_at"`
	WindowMinutes int                    `json:"window_minutes"`
	SliceSeconds  int                    `json:"slice_seconds"`
	Pulse         map[string]int         `json:"pulse"`
	Needs         []BoardRow             `json:"needs"`
	Groups        []BoardGroup           `json:"groups"`
	Finished      []BoardRow             `json:"finished"`
	Idle          int                    `json:"idle"`
	Hidden        int                    `json:"hidden"`
	Load          []int                  `json:"load"`
	Memory        string                 `json:"memory"`
}

func BoardKey(n FlowNode) string { return fmt.Sprintf("%s/%d", n.MachineID, n.ID) }
func boardState(n FlowNode) string {
	if n.EndedAt != nil {
		return "finished"
	}
	s := n.Mixer.Verdict
	if s == "" {
		s = n.Label
	}
	if s == "ended" || s == "dead" {
		return "finished"
	}
	return s
}
func boardRank(s string) int {
	switch s {
	case "waiting":
		return 0
	case "stuck":
		return 1
	case "working":
		return 2
	case "idle":
		return 3
	case "finished":
		return 4
	}
	return 3
}

// Board projects the overview once, indexes family links and then sorts only
// presentation rows. The total row budget includes folded idle rows and needs.
// Historical working cells require retained measurements; a current verdict is
// applied only at the now edge, never painted backwards over unknown history.
func Board(snapshot BoardSnapshot, window time.Duration, now time.Time) BoardView {
	minutes := int(window / time.Minute)
	if minutes != 15 && minutes != 60 && minutes != 240 {
		minutes = 60
	}
	step := time.Duration(minutes) * time.Minute / 60
	edge := now.Truncate(step).Add(step)
	b := BoardView{ObservedAt: now, FromAt: edge.Add(-time.Duration(minutes) * time.Minute), WindowMinutes: minutes, SliceSeconds: int(step / time.Second), Machines: map[string]*BoardGroup{}, Pulse: map[string]int{"waiting": 0, "stuck": 0, "working": 0, "idle": 0, "finished": 0}, Needs: []BoardRow{}, Groups: []BoardGroup{}, Finished: []BoardRow{}, Load: snapshot.Load, Memory: snapshot.Memory}

	totals := map[string]*BoardGroup{}
	branches := map[string]map[string]bool{}
	cwd := map[string]map[string]bool{}
	rows := make([]BoardRow, 0, len(snapshot.Nodes))
	for _, n := range snapshot.Nodes {
		if n.StartedAt.After(now) || n.EndedAt != nil && n.EndedAt.Before(b.FromAt) {
			continue
		}
		state := boardState(n)
		b.Pulse[state]++

		r := BoardRow{Files: snapshot.Files[BoardKey(n)], Key: BoardKey(n), Node: n, Title: n.DisplayTitle(), State: state, Cwd: n.Cwd, Workers: []string{}, Slices: make([]BoardSlice, 60)}
		col := func(at time.Time) int { return int(at.Sub(b.FromAt) / step) }
		for i := range r.Slices {
			at := b.FromAt.Add(time.Duration(i) * step)
			s := "unknown"
			if !at.Add(step).After(n.StartedAt) {
				s = "absent"
			}
			if n.EndedAt != nil && !at.Before(*n.EndedAt) {
				s = "absent"
			}
			r.Slices[i] = BoardSlice{At: at, State: s}
		}
		if n.Observed != nil {
			for i, x := range n.Observed.LogBytesPerMin {
				at := n.Observed.FromAt.Add(time.Duration(i) * time.Minute)
				if at.After(now) || at.Before(b.FromAt) {
					continue
				}
				c := col(at)
				if c >= 0 && c < 60 {
					r.Slices[c].Activity += x
					cpu := i < len(n.Observed.CPUMSPerMin) && n.Observed.CPUMSPerMin[i] > 0
					if x > 0 || cpu {
						r.Slices[c].State = "working"
					}
				}
			}
		}
		var cost float64
		var hasCost bool
		var input, output int64
		var hasInput, hasOutput bool
		var lastTool time.Time
		for _, e := range n.Events {
			if e.TS.Before(b.FromAt) || e.TS.After(now) {
				continue
			}
			c := col(e.TS)
			if c < 0 || c >= 60 {
				continue
			}
			r.Slices[c].Events++
			if FlowEventMark(n.Adapter, e.Kind) == "tool" {
				r.Slices[c].Tools++
				r.Slices[c].State = "working"
				tool := ""
				if e.Metadata != nil {
					tool = e.Metadata.Tool
				}
				if !e.TS.Before(lastTool) {
					r.Node.LastTool = FlowJoin(tool, e.Summary)
					lastTool = e.TS
				}
			}
			if e.Metadata != nil {
				if e.Metadata.InputTokens != nil {
					input += *e.Metadata.InputTokens
					hasInput = true
				}
				if e.Metadata.OutputTokens != nil {
					output += *e.Metadata.OutputTokens
					hasOutput = true
				}
				if e.Metadata.CostUSD != nil {
					cost += *e.Metadata.CostUSD
					hasCost = true
				}
			}
		}
		if hasInput {
			r.Node.InputTokens = &input
		}
		if hasOutput {
			r.Node.OutputTokens = &output
		}
		if hasCost {
			r.Cost = &cost
		}
		for i := range n.Waits {
			w := n.Waits[i]
			if w.StartedAt.After(now) {
				continue
			}
			end := now
			if w.EndedAt != nil {
				end = *w.EndedAt
			}
			if end.Before(b.FromAt) {
				continue
			}
			for c := max(0, col(w.StartedAt)); c <= min(59, col(end)); c++ {
				r.Slices[c].State = "waiting"
			}
			if w.EndedAt == nil && (r.Wait == nil || w.StartedAt.Before(r.Wait.StartedAt)) {
				copy := w
				r.Wait = &copy
				reason := w.Reason
				if reason == "permission" {
					reason = "Permission"
				}
				if reason == "idle_prompt" || reason == "unknown" {
					reason = "Waiting for your reply"
				}
				r.WaitReason = FlowJoin(reason, w.Tool, w.Detail)
			}
		}
		if r.Wait != nil && n.EndedAt == nil && state != "finished" {
			if state != "waiting" {
				b.Pulse[state]--
				b.Pulse["waiting"]++
			}
			r.State = "waiting"
		}
		if r.WaitReason == "" && r.State == "waiting" {
			r.WaitReason = "Waiting for your reply · jump to terminal"
		}
		if n.EndedAt != nil {
			c := col(*n.EndedAt)
			if c >= 0 && c < 60 {
				r.Slices[c].State = "finished"
			}
		} else {
			r.Slices[59].State = r.State
		}

		name := n.Project
		if name == "" {
			name = "Unattached"
		}
		total := totals[name]
		if total == nil {
			total = &BoardGroup{Key: name, Name: name, Spark: make([]int, 60), Branches: []string{}}
			totals[name] = total
			branches[name] = map[string]bool{}
			cwd[name] = map[string]bool{}
		}
		total.Sessions++
		if r.Cost != nil {
			if total.Cost == nil {
				total.Cost = new(float64)
			}
			*total.Cost += *r.Cost
		}
		if n.Branch != "" {
			branches[name][n.Branch] = true
		}
		if n.Cwd != "" {
			cwd[name][n.Cwd] = true
		}
		for i, s := range r.Slices {
			total.Spark[i] += s.Tools
			if s.Activity > 0 {
				total.Spark[i]++
			}
		}
		machine := b.Machines[n.MachineID]
		if machine == nil {
			machine = &BoardGroup{Key: n.MachineID, Name: n.MachineID, Spark: make([]int, 60)}
			b.Machines[n.MachineID] = machine
		}
		machine.Sessions++
		if r.Cost != nil {
			if machine.Cost == nil {
				machine.Cost = new(float64)
			}
			*machine.Cost += *r.Cost
		}
		for i, s := range r.Slices {
			machine.Spark[i] += s.Tools
			if s.Activity > 0 {
				machine.Spark[i]++
			}
		}
		rows = append(rows, r)
	}
	// A complete forest, sorted only by project and start time. Current state
	// never changes a slot. Missing parents and cycles remain visible as roots.
	sort.Slice(rows, func(i, j int) bool {
		a, c := rows[i], rows[j]
		if a.Node.Project != c.Node.Project {
			return a.Node.Project < c.Node.Project
		}
		if !a.Node.StartedAt.Equal(c.Node.StartedAt) {
			return a.Node.StartedAt.Before(c.Node.StartedAt)
		}
		return a.Key < c.Key
	})
	index := map[string]int{}
	for i, r := range rows {
		index[r.Key] = i
	}
	children := map[string][]int{}
	for i := range rows {
		r := &rows[i]
		r.Glyph = BoardGlyph(r.State)
		r.Local = !snapshot.Remote[r.Node.MachineID]
		r.Age = BoardAge(*r, now)
		if r.Node.ParentID != nil {
			key := BoardKey(FlowNode{MachineID: r.Node.MachineID, ID: *r.Node.ParentID})
			if j, ok := index[key]; ok && key != r.Key && rows[j].Node.Project == r.Node.Project {
				seen := map[string]bool{r.Key: true}
				cursor := j
				cycle := false
				for {
					n := rows[cursor].Node
					k := BoardKey(n)
					if seen[k] {
						cycle = true
						break
					}
					seen[k] = true
					if n.ParentID == nil {
						break
					}
					next, ok := index[BoardKey(FlowNode{MachineID: n.MachineID, ID: *n.ParentID})]
					if !ok {
						break
					}
					cursor = next
				}
				if !cycle {
					r.Parent = key
					children[key] = append(children[key], i)
				}
			}
		}
	}
	b.Spark = make([]int, 60)
	groups := map[string]int{}
	var visit func(int, int, string, bool)
	visit = func(i, depth int, root string, last bool) {
		r := rows[i]
		r.Depth = depth
		r.Root = root
		if depth > 0 {
			r.Branch = strings.Repeat("  ", depth-1) + "├"
			if last {
				r.Branch = strings.Repeat("  ", depth-1) + "└"
			}
		}
		for _, j := range children[r.Key] {
			r.Workers = append(r.Workers, rows[j].State)
		}
		name := r.Node.Project
		if name == "" {
			name = "Unattached"
		}
		gi, ok := groups[name]
		if !ok {
			gi = len(b.Groups)
			groups[name] = gi
			g := *totals[name]
			g.Rows = []BoardRow{}
			b.Groups = append(b.Groups, g)
		}
		b.Groups[gi].Rows = append(b.Groups[gi].Rows, r)
		if r.State == "waiting" {
			b.Needs = append(b.Needs, r)
		}
		if r.State == "finished" {
			b.Finished = append(b.Finished, r)
		}
		if r.State == "idle" {
			b.Idle++
		}
		for c, s := range r.Slices {
			b.Spark[c] += s.Events
		}
		for c, j := range children[r.Key] {
			visit(j, depth+1, root, c == len(children[r.Key])-1)
		}
	}
	for i, r := range rows {
		if r.Parent == "" {
			visit(i, 0, r.Key, true)
		}
	}
	for i := range b.Groups {
		g := &b.Groups[i]
		for branch := range branches[g.Key] {
			g.Branches = append(g.Branches, branch)
		}
		sort.Strings(g.Branches)
		g.Worktrees = max(0, len(cwd[g.Key])-1)
	}
	sort.SliceStable(b.Needs, func(i, j int) bool {
		a, c := b.Needs[i], b.Needs[j]
		at, ct := a.Node.LastActivityAt, c.Node.LastActivityAt
		if a.Wait != nil {
			at = a.Wait.StartedAt
		}
		if c.Wait != nil {
			ct = c.Wait.StartedAt
		}
		if !at.Equal(ct) {
			return at.Before(ct)
		}
		return a.Key < c.Key
	})
	b.buildMap(snapshot.Remote)

	return b
}

// BoardStrip resamples all sixty slices to the available terminal columns.
func BoardStrip(row BoardRow, columns int) string {
	columns = max(1, columns)
	out := make([]rune, columns)
	for i := range out {
		lo := i * len(row.Slices) / columns
		hi := max(lo+1, (i+1)*len(row.Slices)/columns)
		hi = min(hi, len(row.Slices))
		ch := ' '
		priority := -1
		for _, s := range row.Slices[lo:hi] {
			v, p := '░', 0
			switch s.State {
			case "absent":
				v, p = ' ', 0
			case "idle":
				v, p = '─', 1
			case "working":
				v, p = '━', 2
			case "stuck":
				v, p = '░', 3
			case "waiting":
				v, p = '◆', 4
			case "finished":
				v, p = '✓', 5
			}
			if s.Tools > 0 && p < 4 {
				v, p = '┿', 3
			}
			if p >= priority {
				ch, priority = v, p
			}
		}
		out[i] = ch
	}
	return string(out)
}

// WithPrompts associates only proven pending permission requests with a wait.
func (b *BoardView) WithPrompts(prompts map[string]string) {
	for gi := range b.Groups {
		for ri := range b.Groups[gi].Rows {
			r := &b.Groups[gi].Rows[ri]
			r.PromptID = prompts[r.Key]
		}
	}
	for i := range b.Needs {
		b.Needs[i].PromptID = prompts[b.Needs[i].Key]
	}
}

// BoardSpark keeps project and machine rhythms to a fixed twelve-cell measure.
func BoardSpark(buckets []int) string {
	out := make([]int, 12)
	if len(buckets) == 0 {
		return "────────────"
	}
	for i, v := range buckets {
		c := min(11, i*12/len(buckets))
		out[c] += v
	}
	return FlowSpark(out)
}
