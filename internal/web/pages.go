package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
)

type navItem struct{ Path, Name string }

var navigation = []navItem{{"/", "Home"}, {"/agents", "Agents"}, {"/projects", "Projects"}, {"/inbox", "Inbox"}, {"/orphans", "Orphans"}, {"/machines", "Machines"}, {"/cleanup", "Cleanup"}}

type attention struct {
	ID                             int64
	State, Project, Title, Machine string
	At                             time.Time
}
type stateCount struct {
	Label string
	Count int
}
type projectCard struct {
	view.ProjectRow
	Branch, Title string
	Agents        []view.AgentRow
}
type pageData struct {
	viewer.Snapshot
	Title, Page, Scope, RefreshURL, CleanupError string
	Nav                                          []navItem
	Agent                                        view.AgentView
	Project                                      view.WhereView
	AgentRows                                    []view.AgentRow
	Needs                                        []attention
	States                                       []stateCount
	ProjectCards                                 []projectCard
	Ready, Refused                               int
	History                                      int64
}

func (s *Server) loadPage(r *http.Request) (pageData, int, error) {
	p := pageData{Nav: navigation, Page: r.URL.Path, Scope: r.URL.Query().Get("machine")}
	switch p.Page {
	case "/", "/agents", "/projects", "/inbox", "/orphans", "/machines", "/cleanup":
	default:
		if !strings.HasPrefix(p.Page, "/agents/") && !strings.HasPrefix(p.Page, "/projects/") {
			return p, 404, fmt.Errorf("page not found")
		}
	}
	snap, err := s.source.LoadScope(r.Context(), store.Scope{})
	if err != nil {
		return p, 500, err
	}
	if p.Scope == "" {
		p.Scope = s.opts.Machine
	}
	if p.Scope == "local" {
		p.Scope = snap.LocalMachineID
	}
	if p.Scope == "" {
		p.Scope = "all"
	}
	if p.Scope != "all" {
		matches := []string{}
		for _, m := range snap.Machines.Rows {
			if p.Scope == m.MachineID || p.Scope == m.Name || p.Scope == m.Hostname || m.DeviceID != "" && strings.HasPrefix(strings.ToUpper(m.DeviceID), strings.ToUpper(p.Scope)) {
				matches = append(matches, m.MachineID)
			}
		}
		if len(matches) != 1 {
			return p, 400, fmt.Errorf("unknown or ambiguous machine scope")
		}
		p.Scope = matches[0]
		snap, err = s.source.LoadScope(r.Context(), store.Scope{Machines: []string{p.Scope}})
		if err != nil {
			return p, 500, err
		}
	}
	p.Snapshot = snap
	p.RefreshURL = p.Page + "?" + url.Values{"machine": {p.Scope}}.Encode()
	p.Title = "Home"
	for _, n := range navigation {
		if p.Page == n.Path {
			p.Title = n.Name
		}
	}
	if strings.HasPrefix(p.Page, "/agents/") {
		id, err := strconv.ParseInt(strings.TrimPrefix(p.Page, "/agents/"), 10, 64)
		d, ok := snap.AgentDetails[id]
		if err != nil || !ok {
			return p, 404, fmt.Errorf("agent not found")
		}
		p.Agent, p.Title = d, "Agent detail"
	}
	if strings.HasPrefix(p.Page, "/projects/") {
		id, err := strconv.ParseInt(strings.TrimPrefix(p.Page, "/projects/"), 10, 64)
		d, ok := snap.ProjectDetails[id]
		if err != nil || !ok {
			return p, 404, fmt.Errorf("project not found")
		}
		p.Project, p.Title = d, "Project detail"
	}
	if (p.Page == "/" || p.Page == "/cleanup") && (p.Scope == "all" || p.Scope == snap.LocalMachineID) && s.opts.CleanupList != nil {
		p.Cleanup, err = s.opts.CleanupList(r.Context())
		if err != nil {
			p.CleanupError = err.Error()
		}
	}
	p.prepare()
	return p, 200, nil
}

func flatten(rows []view.AgentRow) []view.AgentRow {
	result := []view.AgentRow{}
	for _, r := range rows {
		result = append(result, r)
		result = append(result, flatten(r.Children)...)
	}
	return result
}

func (p *pageData) prepare() {
	visibleInbox := []view.InboxRow{}
	for _, i := range p.Inbox.Rows {
		if i.AckedAt == nil {
			visibleInbox = append(visibleInbox, i)
		}
	}
	p.Inbox.Rows = visibleInbox
	p.AgentRows = flatten(p.Agents.Rows)
	counts := map[string]int{}
	for _, a := range p.AgentRows {
		counts[strings.TrimPrefix(a.Label, "~")]++
	}
	for _, label := range []string{"working", "waiting", "stalled?", "idle", "finished", "dead", "unknown"} {
		if counts[label] > 0 || label != "dead" && label != "unknown" {
			p.States = append(p.States, stateCount{label, counts[label]})
		}
	}
	seen := map[int64]bool{}
	for _, i := range p.Inbox.Rows {
		if i.AckedAt != nil || i.Kind != "waiting" && i.Kind != "finished" && i.Kind != "worker-finished" {
			continue
		}
		label := i.Kind
		if i.Kind == "waiting" {
			seen[i.SessionID] = true
			if d := p.AgentDetails[i.SessionID]; strings.TrimPrefix(d.Label, "~") == "stalled?" {
				label = d.Label
			}
		}
		p.Needs = append(p.Needs, attention{i.SessionID, label, i.Project, i.Summary, i.MachineName, i.TS})
	}
	for _, a := range p.AgentRows {
		state := strings.TrimPrefix(a.Label, "~")
		if !seen[a.ID] && (state == "waiting" || state == "stalled?") {
			p.Needs = append(p.Needs, attention{a.ID, a.Label, a.Project, a.Title, a.MachineName, p.AgentDetails[a.ID].Session.LastActivityAt})
		}
	}
	sort.SliceStable(p.Needs, func(i, j int) bool { return p.Needs[i].At.After(p.Needs[j].At) })
	for _, project := range p.Projects.Rows {
		card := projectCard{ProjectRow: project}
		for _, a := range p.AgentRows {
			if a.MachineID == project.MachineID && a.Project == project.Status.Repo.Name {
				card.Agents = append(card.Agents, a)
				if card.Branch == "" {
					card.Branch = a.Branch
				}
				if card.Title == "" {
					card.Title = a.Title
				}
			}
		}
		d := p.ProjectDetails[project.Status.Repo.ID]
		if card.Branch == "" && len(d.WorktreeRows) > 0 {
			card.Branch = d.WorktreeRows[0].Branch
		}
		if len(d.Handoffs) > 0 && d.Handoffs[0].Text != "" {
			card.Title = d.Handoffs[0].Text
		} else if card.Title == "" && len(project.Status.Where) > 0 {
			card.Title = project.Status.Where[0].Text
		}
		p.ProjectCards = append(p.ProjectCards, card)
	}
	sort.SliceStable(p.ProjectCards, func(i, j int) bool {
		a, b := p.ProjectCards[i], p.ProjectCards[j]
		if (a.Status.LiveAgents > 0) != (b.Status.LiveAgents > 0) {
			return a.Status.LiveAgents > 0
		}
		return a.Status.Repo.Name < b.Status.Repo.Name
	})
	for _, c := range p.Cleanup {
		if c.Kind == cleanup.History {
			p.History += c.Size
		}
		if c.Kind == cleanup.Worktree {
			if c.Status == "ready" {
				p.Ready++
			} else {
				p.Refused++
			}
		}
	}
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"familyData": func(row view.AgentRow, scope string) any {
			return struct {
				Row   view.AgentRow
				Scope string
			}{row, scope}
		},
		"chartData": func(buckets []view.ActivityBucket) string {
			times, events, loads := []any{}, []any{}, []any{}
			for _, b := range buckets {
				times = append(times, b.TS.Unix())
				events = append(events, b.Events)
				if b.Sample == nil {
					loads = append(loads, nil)
				} else {
					loads = append(loads, b.Sample.Load1)
				}
			}
			data, _ := json.Marshal([][]any{times, events, loads})
			return string(data)
		},
		"stateClass": func(s string) string {
			switch strings.TrimPrefix(s, "~") {
			case "working", "ready", "live", "local":
				return "working"
			case "waiting", "needs-review":
				return "waiting"
			case "stalled?", "dead", "refused", "error":
				return "stalled"
			case "idle":
				return "idle"
			default:
				return "finished"
			}
		},
		"bytes": humanBytes,
		"stamp": func(t time.Time) string {
			if t.IsZero() {
				return "Unavailable"
			}
			return t.Local().Format("Jan 2, 15:04:05")
		},
		"duration":    func(d time.Duration) string { return d.Round(time.Second).String() },
		"machineName": func(m view.MachineRow) string { return view.MachineName(m.Machine) },
		"link":        func(path, scope string) string { return path + "?" + url.Values{"machine": {scope}}.Encode() },
		"agentPath":   func(id int64) string { return fmt.Sprint("/agents/", id) },
		"projectPath": func(id int64) string { return fmt.Sprint("/projects/", id) },
	}
}

func humanBytes(value any) string {
	var n float64
	switch v := value.(type) {
	case uint64:
		n = float64(v)
	case int64:
		n = float64(v)
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	i := 0
	for n >= 1024 && i < len(units)-1 {
		n /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", n, units[i])
}
