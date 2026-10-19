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

type projectCard struct {
	view.ProjectRow
	Branch, Title string
	States        []view.StateCount
}
type pageData struct {
	viewer.Snapshot
	Title, Page, Scope, RefreshURL, CleanupError string
	Nav                                          []navItem
	Agent                                        view.AgentView
	Project                                      view.WhereView
	AgentRows                                    []view.AgentRow
	Needs                                        []view.Attention
	NeedsMore                                    int
	All                                          bool
	StaleSources, StaleDetails, PendingSources   string
	States                                       []view.StateCount
	ProjectCards                                 []projectCard
	Ready, Refused                               int
	History                                      int64
}

func (s *Server) loadPage(r *http.Request) (pageData, int, error) {
	p := pageData{Nav: navigation, Page: r.URL.Path, Scope: r.URL.Query().Get("machine"), All: r.URL.Query().Get("all") == "1"}
	switch p.Page {
	case "/", "/agents", "/projects", "/inbox", "/orphans", "/machines", "/cleanup":
	default:
		if !strings.HasPrefix(p.Page, "/agents/") && !strings.HasPrefix(p.Page, "/projects/") {
			return p, 404, fmt.Errorf("page not found")
		}
	}
	snap, err := s.source.LoadScope(r.Context(), store.Scope{}, p.All)
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
		snap, err = s.source.LoadScope(r.Context(), store.Scope{Machines: []string{p.Scope}}, p.All)
		if err != nil {
			return p, 500, err
		}
	}
	p.Snapshot = snap
	p.RefreshURL = pageLink(p.Page, p.Scope, p.All)
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
	if (p.Scope == "all" || p.Scope == snap.LocalMachineID) && s.opts.CleanupList != nil {
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
			p.States = append(p.States, view.StateCount{Label: label, Count: counts[label]})
		}
	}
	p.Needs = view.NeedsYou(p.Inbox.Rows, p.Agents.Rows, p.AgentDetails)
	p.NeedsMore = max(0, len(p.Needs)-8)
	p.Needs = p.Needs[:min(8, len(p.Needs))]
	names := []string{}
	pending := []string{}
	for name, f := range p.Freshness {
		if f.Stale {
			names = append(names, name)
		} else if f.Status == "not-yet-receiving" {
			pending = append(pending, name)
		}
	}
	sort.Strings(names)
	sort.Strings(pending)
	p.StaleSources = strings.Join(names, ", ")
	details := []string{}
	for _, name := range names {
		f := p.Freshness[name]
		details = append(details, fmt.Sprintf("%s: last successful reading %s; %s", name, f.LastOKAt.Format(time.RFC3339), f.LastError))
	}
	p.StaleDetails = strings.Join(details, "\n")
	p.PendingSources = strings.Join(pending, ", ")
	for _, project := range view.SortedProjects(p.Projects.Rows) {
		card := projectCard{ProjectRow: project}
		agents := []view.AgentRow{}
		for _, a := range p.AgentRows {
			if a.MachineID == project.MachineID && a.Project == project.Status.Repo.Name {
				agents = append(agents, a)
				if card.Branch == "" {
					card.Branch = a.Branch
				}
				if card.Title == "" {
					card.Title = a.Title
				}
			}
		}
		card.States = view.StateCounts(agents)
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
		"familyData": func(row view.AgentRow, scope string, all bool) any {
			return struct {
				Row   view.AgentRow
				Scope string
				All   bool
			}{row, scope, all}
		},
		"navActive": func(path, page string) bool { return path == page || path != "/" && strings.HasPrefix(page, path+"/") },
		"navCount": func(p pageData, name string) int {
			switch name {
			case "Agents":
				return len(p.AgentRows)
			case "Projects":
				return len(p.Projects.Rows)
			case "Inbox":
				return len(p.Inbox.Rows)
			case "Orphans":
				return len(p.Orphans.Rows)
			case "Machines":
				return len(p.Machines.Rows)
			case "Cleanup":
				return len(p.Cleanup)
			}
			return 0
		},
		"lower": strings.ToLower,
		"add":   func(a, b int) int { return a + b },
		"initial": func(s string) string {
			if s == "" {
				return "?"
			}
			if s == "codex" {
				return "X"
			}
			return strings.ToUpper(string([]rune(s)[0]))
		},
		"stateSummary": func(states []view.StateCount) string {
			parts := []string{}
			for _, s := range states {
				parts = append(parts, fmt.Sprintf("%d %s", s.Count, s.Label))
			}
			return strings.Join(parts, " · ")
		},
		"loadLevel": view.LoadLevel,
		"cores": func(buckets []view.ActivityBucket) int {
			for i := len(buckets) - 1; i >= 0; i-- {
				if buckets[i].Sample != nil {
					return buckets[i].Sample.Cores
				}
			}
			return 0
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
		"bytes":      humanBytes,
		"gaugeLevel": view.GaugeLevel,
		"percent":    view.GaugePercent,
		"hasSamples": view.ActivityHasSamples,
		"hasEvents":  view.ActivityHasEvents,
		"relative": func(at, now time.Time) string {
			if at.IsZero() {
				return "age unavailable"
			}
			age := max(time.Duration(0), now.Sub(at))
			if age < time.Minute {
				return "just now"
			}
			if age < time.Hour {
				return fmt.Sprintf("%dm ago", int(age/time.Minute))
			}
			if age < 24*time.Hour {
				return fmt.Sprintf("%dh ago", int(age/time.Hour))
			}
			return fmt.Sprintf("%dd ago", int(age/(24*time.Hour)))
		},
		"stamp": func(t time.Time) string {
			if t.IsZero() {
				return "Unavailable"
			}
			return t.Local().Format("Jan 2, 15:04:05")
		},
		"duration":    func(d time.Duration) string { return d.Round(time.Second).String() },
		"machineName": func(m view.MachineRow) string { return view.MachineName(m.Machine) },
		"link":        pageLink,
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

func pageLink(path, scope string, all ...bool) string {
	q := url.Values{"machine": {scope}}
	if len(all) > 0 && all[0] {
		q.Set("all", "1")
	}
	return path + "?" + q.Encode()
}
