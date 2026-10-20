package web

import (
	"context"
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
	"github.com/labstack/driga/internal/inbox"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
)

type navItem struct{ Path, Name string }

var navigation = []navItem{{"/", "Home"}, {"/agents", "Agents"}, {"/projects", "Projects"}, {"/inbox", "Inbox"}, {"/orphans", "Orphans"}, {"/machines", "Machines"}, {"/cleanup", "Cleanup"}, {"/flow", "Flow"}, {"/chat", "Chat"}}

type projectCard struct {
	view.ProjectRow
	Branch, Title string
	States        []view.StateCount
}
type pageData struct {
	CSRF                   string
	ChatEphemeral          bool
	Flow                   view.FlowView
	FlowPrevious, FlowNext string
	FlowExpanded           bool
	FlowToggle             string
	LastEvents             string
	AttentionOnly          bool
	viewer.Snapshot
	Title, Page, Scope, RefreshURL, CleanupError string
	Nav                                          []navItem
	Agent                                        view.AgentView
	Project                                      view.WhereView
	AgentRows                                    []view.AgentRow
	Needs                                        []view.Attention
	NeedsMore                                    int
	InboxWaiting, InboxRecent                    []view.InboxRow
	All                                          bool
	StaleSources, StaleDetails, PendingSources   string
	States                                       []view.StateCount
	ProjectCards                                 []projectCard
	Ready, Refused                               int
	History                                      int64
}

func (s *Server) loadPage(r *http.Request) (pageData, int, error) {
	p := pageData{CSRF: s.csrf, ChatEphemeral: s.opts.ChatEphemeral, Nav: navigation, Page: r.URL.Path, Scope: r.URL.Query().Get("machine"), All: r.URL.Query().Get("all") == "1"}
	switch p.Page {
	case "/", "/activity", "/agents", "/projects", "/inbox", "/orphans", "/machines", "/cleanup", "/flow", "/chat":
	default:
		if !strings.HasPrefix(p.Page, "/agents/") && !strings.HasPrefix(p.Page, "/projects/") {
			return p, 404, fmt.Errorf("page not found")
		}
	}
	p.AttentionOnly = r.URL.Query().Get("attention") == "1"
	snap, err := s.cachedLoad(r.Context(), store.Scope{}, p.All)
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
		snap, err = s.cachedLoad(r.Context(), store.Scope{Machines: []string{p.Scope}}, p.All)
		if err != nil {
			return p, 500, err
		}
	}
	p.Snapshot = snap
	if p.Scope != "all" {
		for _, m := range snap.Machines.Rows {
			if m.MachineID == p.Scope {
				p.Hostname = view.MachineName(m.Machine)
			}
		}
	} else if len(snap.Machines.Rows) > 1 {
		p.Hostname = "all machines"
	}
	p.RefreshURL = pageLink(p.Page, p.Scope, p.All)
	if p.AttentionOnly {
		p.RefreshURL += "&attention=1"
	}
	q := r.URL.Query()
	refresh, _ := url.Parse(p.RefreshURL)
	values := refresh.Query()
	for _, key := range []string{"state", "project", "why", "id"} {
		if q.Has(key) {
			values.Set(key, q.Get(key))
		}
	}
	if p.Page == "/flow" && q.Has("quiet") {
		values.Set("quiet", q.Get("quiet"))
	}
	refresh.RawQuery = values.Encode()
	p.RefreshURL = refresh.String()
	if p.Page == "/projects" && q.Get("project") != "" {
		matches := []view.ProjectRow{}
		for _, row := range snap.Projects.Rows {
			if row.Status.Repo.Name == q.Get("project") || row.Status.Repo.Path == q.Get("project") {
				matches = append(matches, row)
			}
		}
		if len(matches) != 1 {
			return p, 400, fmt.Errorf("unknown or ambiguous project")
		}
		p.Page = fmt.Sprint("/projects/", matches[0].Status.Repo.ID)
	}
	p.Title = "Home"
	if p.Page == "/activity" {
		p.Title = "Activity"
	}
	for _, n := range navigation {
		if p.Page == n.Path {
			p.Title = n.Name
		}
	}
	if strings.HasPrefix(p.Page, "/agents/") {
		id, err := strconv.ParseInt(strings.TrimPrefix(p.Page, "/agents/"), 10, 64)
		d, ok := snap.AgentDetails[id]
		if err == nil {
			if source, supported := s.source.(viewer.DetailSource); supported {
				// Keep details within the overview's project and machine selection.
				visible := false
				for _, a := range flatten(snap.Agents.Rows) {
					visible = visible || a.ID == id
				}
				for _, i := range snap.Inbox.Rows {
					visible = visible || i.SessionID == id
				}
				if visible {
					scope := store.Scope{}
					if p.Scope != "all" {
						scope.Machines = []string{p.Scope}
					}
					d, err = source.LoadAgent(r.Context(), scope, id)
					ok = err == nil
				}
			}
		}
		if err != nil || !ok {
			return p, 404, fmt.Errorf("agent not found")
		}
		p.Agent, p.Title = d, "Agent detail"
	}
	if strings.HasPrefix(p.Page, "/projects/") {
		id, err := strconv.ParseInt(strings.TrimPrefix(p.Page, "/projects/"), 10, 64)
		d, ok := snap.ProjectDetails[id]
		if err == nil {
			if source, supported := s.source.(viewer.DetailSource); supported {
				for _, row := range snap.Projects.Rows {
					if row.Status.Repo.ID == id {
						d, err = source.LoadProject(r.Context(), store.Scope{Machines: []string{row.MachineID}}, id, p.All)
						ok = err == nil
						break
					}
				}
			}
		}
		if err != nil || !ok {
			return p, 404, fmt.Errorf("project not found")
		}
		p.Project, p.Title = d, "Project detail"
	}
	if (p.Scope == "all" || p.Scope == snap.LocalMachineID) && s.opts.CleanupList != nil {
		p.Cleanup, err = s.cachedCleanup(r.Context())
		if err != nil {
			p.CleanupError = err.Error()
		}
	}
	if p.Page == "/flow" {
		p.FlowExpanded = r.URL.Query().Get("quiet") == "1"
		q := r.URL.Query()
		if p.FlowExpanded {
			q.Del("quiet")
		} else {
			q.Set("quiet", "1")
		}
		p.FlowToggle = "/flow?" + q.Encode()
		id := int64(0)
		if q.Has("id") {
			id, err = strconv.ParseInt(q.Get("id"), 10, 64)
			if err != nil || id <= 0 {
				return p, 400, fmt.Errorf("invalid session id")
			}
		}
		if source, ok := s.source.(viewer.FlowSource); ok {
			scope := store.Scope{}
			if p.Scope != "all" {
				scope.Machines = []string{p.Scope}
			}
			minutes := 60
			if q.Has("minutes") {
				minutes, err = strconv.Atoi(q.Get("minutes"))
				if err != nil || minutes != 15 && minutes != 60 && minutes != 240 {
					return p, 400, fmt.Errorf("minutes must be 15, 60 or 240")
				}
			}
			if window, ok := s.source.(viewer.FlowWindowSource); ok {
				p.Flow, err = window.LoadFlowWindow(r.Context(), scope, id, minutes)
			} else {
				p.Flow, err = source.LoadFlow(r.Context(), scope, id)
			}
			if err != nil {
				return p, 404, err
			}
		}
		if p.Flow.Lead != nil {
			p.Tail = p.Flow.Tail
			choices := []view.AgentRow{}
			for _, a := range snap.Agents.Rows {
				if a.Origin != "hosted" {
					choices = append(choices, a)
				}
			}
			for i, a := range choices {
				if a.ID == p.Flow.Lead.ID {
					p.FlowPrevious = pageLink("/flow", p.Scope, p.All) + "&id=" + fmt.Sprint(choices[(i+len(choices)-1)%len(choices)].ID)
					p.FlowNext = pageLink("/flow", p.Scope, p.All) + "&id=" + fmt.Sprint(choices[(i+1)%len(choices)].ID)
				}
			}
		}
	}
	p.prepare()
	if p.Page == "/agents" {
		rows := []view.AgentRow{}
		for _, a := range p.AgentRows {
			if q.Get("state") != "" && strings.TrimSuffix(strings.TrimPrefix(a.Label, "~"), "?") != q.Get("state") {
				continue
			}
			if q.Get("project") != "" && a.Project != q.Get("project") {
				continue
			}
			rows = append(rows, a)
		}
		p.AgentRows = rows
	}
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
		if i.AckedAt == nil && i.EndedAt == nil {
			visibleInbox = append(visibleInbox, i)
		}
	}
	p.Inbox.Rows = visibleInbox
	for _, row := range visibleInbox {
		if inbox.Active(row.InboxItem) {
			row.Waited = inbox.Waited(row.InboxItem, p.ObservedAt)
			p.InboxWaiting = append(p.InboxWaiting, row)
		} else {
			p.InboxRecent = append(p.InboxRecent, row)
		}
	}
	sort.SliceStable(p.InboxWaiting, func(i, j int) bool {
		a, b := p.InboxWaiting[i], p.InboxWaiting[j]
		if a.Waited != b.Waited {
			return a.Waited > b.Waited
		}
		return inbox.Less(a.InboxItem, b.InboxItem)
	})
	sort.SliceStable(p.InboxRecent, func(i, j int) bool {
		return inbox.Less(p.InboxRecent[i].InboxItem, p.InboxRecent[j].InboxItem)
	})
	p.AgentRows = flatten(p.Agents.Rows)
	counts := map[string]int{}
	for _, a := range p.AgentRows {
		counts[strings.TrimSuffix(strings.TrimPrefix(a.Label, "~"), "?")]++
	}
	for _, label := range []string{"working", "waiting", "stalled", "idle", "finished", "dead", "unknown"} {
		if counts[label] > 0 || label != "dead" && label != "unknown" {
			p.States = append(p.States, view.StateCount{Label: label, Count: counts[label]})
		}
	}
	p.Needs = view.NeedsYou(p.Inbox.Rows, p.Agents.Rows, p.AgentDetails)
	if !p.AttentionOnly {
		p.NeedsMore = max(0, len(p.Needs)-8)
		p.Needs = p.Needs[:min(8, len(p.Needs))]
	} else {
		p.Title = "Needs you"
	}
	names := []string{}
	pending := []string{}
	for name, f := range p.Freshness {
		if f.Stale {
			names = append(names, name)
		} else if f.Status == "not_yet_receiving" {
			pending = append(pending, name)
		}
	}
	sort.Strings(names)
	sort.Strings(pending)
	p.StaleSources = strings.Join(names, ", ")
	events := []string{}
	for name, f := range p.Freshness {
		if f.Status == "last_event" {
			events = append(events, fmt.Sprintf("%s: last event %s", name, f.LastOKAt.Format(time.RFC3339)))
		}
	}
	sort.Strings(events)
	p.LastEvents = strings.Join(events, " · ")
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
			if a.MachineID == project.MachineID && a.RepoID == project.Status.Repo.ID {
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
		if card.Branch == "" {
			card.Branch = project.Branch
		}
		if project.LatestHandoff != "" {
			card.Title = project.LatestHandoff
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
		"mcpSummary": view.MCPCommandSummary,
		"mcpDetails": view.MCPCommandDetails,
		"inboxKey": func(row view.InboxRow) string {
			key, _ := json.Marshal([]string{row.MachineID, row.ID})
			return string(key)
		},
		"inboxTable": func(rows []view.InboxRow, p pageData, recent bool) any {
			return struct {
				Rows        []view.InboxRow
				Scope       string
				All, Recent bool
				ObservedAt  time.Time
			}{rows, p.Scope, p.All, recent, p.ObservedAt}
		},
		"waitingHeader": view.WaitingHeader,
		"dollars": func(v *float64) string {
			if v == nil {
				return ""
			}
			return fmt.Sprintf("%.4f", *v)
		},
		"flowSpark":       view.FlowSpark,
		"observationPath": view.ObservationPath,
		"flowJoin":        view.FlowJoin,
		"flowWorkers": func(v view.FlowView, expanded bool, now time.Time) view.FlowWorkers {
			return v.Workers(5, expanded, now)
		},
		"signalBars": func(strength float64) string {
			n := max(0, min(18, int(strength*18+0.5)))
			return strings.Repeat("▮", n) + strings.Repeat("▯", 18-n)
		},
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
		"clock": func(t time.Time) string { return t.Local().Format("15:04:05") },
		"add":   func(a, b int) int { return a + b },
		"initial": func(s string) string {
			if s == "" {
				return "?"
			}
			if s == "copilot" {
				return "P"
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
			case "waiting", "needs_review":
				return "waiting"
			case "stalled", "dead", "refused", "error":
				return "stalled"
			case "idle":
				return "idle"
			default:
				return "finished"
			}
		},
		"bytes":      humanBytes,
		"friendly":   func(v any) string { return view.FriendlyEnum(fmt.Sprint(v)) },
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
	case *uint64:
		if v != nil {
			n = float64(*v)
		}
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

type snapshotKey struct {
	scope string
	all   bool
}
type cachedSnapshot struct {
	snap viewer.Snapshot
	at   time.Time
}

// Cache only overview data. Fifteen-second expiry also refreshes ages without commits.
func (s *Server) cachedLoad(ctx context.Context, scope store.Scope, all bool) (viewer.Snapshot, error) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	version, err := s.source.DataVersion(ctx)
	if err != nil {
		return viewer.Snapshot{}, err
	}
	if version != s.cacheVersion {
		s.cache = map[snapshotKey]cachedSnapshot{}
		s.cacheVersion = version
	}
	key := snapshotKey{scope: strings.Join(scope.Machines, "\x00"), all: all}
	if scope.Machines != nil {
		key.scope = "selected:" + key.scope
	}
	now := time.Now()
	if c, ok := s.cache[key]; ok && now.Sub(c.at) < 15*time.Second {
		return c.snap, nil
	}
	snap, err := s.source.LoadScope(ctx, scope, all)
	if err == nil {
		if len(s.cache) >= 32 {
			s.cache = map[snapshotKey]cachedSnapshot{}
		}
		s.cache[key] = cachedSnapshot{snap, now}
	}
	return snap, err
}
func (s *Server) cachedCleanup(ctx context.Context) ([]cleanup.Candidate, error) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.cleanupAt.IsZero() || time.Since(s.cleanupAt) >= time.Minute {
		s.cleanupRows, s.cleanupErr = s.opts.CleanupList(ctx)
		s.cleanupAt = time.Now()
	}
	return s.cleanupRows, s.cleanupErr
}
