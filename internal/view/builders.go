package view

import (
	"errors"
	"fmt"
	"github.com/labstack/driga/internal/pathx"
	"sort"
	"strings"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/inbox"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func sessions(r Reader, f Filter, now time.Time) ([]model.Session, error) {
	q := store.SessionFilter{RepoPath: f.Project}
	if f.All {
		q.EndedSince = now.Add(-24 * time.Hour)
	}
	ss, err := r.Sessions(q)
	if err != nil {
		return nil, err
	}
	result := []model.Session{}
	for _, s := range visibleSessions(ss, f.All, now) {
		if !f.Since.IsZero() && s.LastActivityAt.Before(f.Since) {
			continue
		}
		result = append(result, s)
	}
	return result, nil
}

func projectName(s model.Session, repos []model.Repo) string {
	for _, r := range repos {
		if s.RepoID != nil && *s.RepoID == r.ID {
			return r.Name
		}
	}
	if strings.Contains(s.Origin, "/temporary") || within(s.Cwd, "/temporary") {
		return "temporary"
	}
	if s.Cwd == "" {
		return ""
	}
	return pathx.RecordedBase(s.Cwd)
}

func procTree(s model.Session, idx derive.ProcIndex) []model.Proc {
	p, ok := idx.Get(s.PID)
	if !ok || s.PID <= 0 || (!s.ProcStart.IsZero() && !derive.SameStart(p.StartTime, s.ProcStart)) {
		return []model.Proc{}
	}
	return append([]model.Proc{p}, idx.Descendants(s.PID)...)
}

// Agents builds families in activity order, retaining children next to parents.
func agentsLocal(r Reader, f AgentFilter, now time.Time) (AgentsView, error) {
	v := AgentsView{Rows: []AgentRow{}}
	ss, err := sessions(r, f, now)
	if err != nil {
		return v, err
	}
	repos, err := r.Repos()
	if err != nil {
		return v, err
	}
	procs, err := r.Procs()
	if err != nil {
		return v, err
	}
	idx := derive.NewProcIndex(procs)
	items, err := r.Inbox(time.Time{}, false)
	if err != nil {
		return v, err
	}
	// Worker records retain their root at completion time, even after the
	// collector can no longer reconstruct ancestry from a departed broker.
	all, err := r.Sessions(store.SessionFilter{IncludeEnded: true})
	if err != nil {
		return v, err
	}
	parents := map[int64]int64{}
	completed := map[int64][]model.CompletionEvent{}
	counts := map[int64]int{}
	for _, s := range all {
		if s.ParentID != nil {
			parents[s.ID] = *s.ParentID
		}
	}
	for _, item := range items {
		if f.CaptureText != "" {
			item = inbox.Policy(item, f.CaptureText)
		}
		if item.Kind == "worker_finished" {
			counts[item.SessionID]++
		} else if item.Kind == "finished" {
			completed[item.SessionID] = append(completed[item.SessionID], model.CompletionEvent{ID: item.ID, TS: item.TS})
		}
	}
	for i := range all {
		all[i].Completions = completed[all[i].ID]
	}
	for parent, count := range derive.FinishedChildCounts(all, parents, nil, time.Time{}) {
		counts[parent] += count
	}
	rows := map[int64]AgentRow{}
	children := map[int64][]int64{}
	for _, s := range ss {
		title := s.Title
		if title == "" {
			title = s.LastPrompt
		}
		row := AgentRow{StartedFrom: s.StartedFrom, MachineID: r.MachineID(), ID: s.ID, Heuristic: s.Heuristic, Label: derive.Display(s.Label, s.Heuristic), Adapter: s.Adapter, Origin: s.Origin, Project: projectName(s, repos), Branch: s.Branch, Title: title, Age: sessionAge(s, now), Evidence: s.Evidence, ObservedAt: now, Children: []AgentRow{}, FinishedChildren: counts[s.ID]}
		if s.RepoID != nil {
			row.RepoID = *s.RepoID
		}
		row.LastActivityAt = s.LastActivityAt
		row.TitleFromPrompt = s.Title == ""
		for _, p := range procTree(s, idx) {
			row.Mem += p.RSS
		}
		rows[s.ID] = row
	}
	roots := []int64{}
	for _, s := range ss {
		p := parents[s.ID]
		if _, ok := rows[p]; p != s.ID && ok {
			children[p] = append(children[p], s.ID)
		} else {
			roots = append(roots, s.ID)
		}
	}
	visited := map[int64]bool{}
	var build func(int64) AgentRow
	build = func(id int64) AgentRow {
		visited[id] = true
		row := rows[id]
		for _, c := range children[id] {
			if !visited[c] {
				row.Children = append(row.Children, build(c))
			}
		}
		return row
	}
	for _, id := range roots {
		if !visited[id] {
			v.Rows = append(v.Rows, build(id))
		}
	}
	// Malformed parent cycles must neither hide sessions nor recurse forever.
	for _, s := range ss {
		if !visited[s.ID] {
			v.Rows = append(v.Rows, build(s.ID))
		}
	}
	return v, nil
}

// Agent reads one session and its last day's files, process tree and events.
func agentLocal(r Reader, id int64, now time.Time) (AgentView, error) {
	v := AgentView{MachineID: r.MachineID(), ObservedAt: now, Files: []FileRow{}, Procs: []ProcRow{}, Events: []EventRow{}}
	s, err := r.Session(id)
	if err != nil {
		return v, err
	}
	v.Session = s
	v.Label = derive.Display(s.Label, s.Heuristic)
	v.Heuristic = s.Heuristic
	files, err := r.SessionFiles(now.Add(-24 * time.Hour))
	if err != nil {
		return v, err
	}
	for _, file := range files[id] {
		v.Files = append(v.Files, FileRow{MachineID: r.MachineID(), FileTouch: file})
	}
	procs, err := r.Procs()
	if err != nil {
		return v, err
	}
	for _, proc := range procTree(s, derive.NewProcIndex(procs)) {
		v.Procs = append(v.Procs, ProcRow{MachineID: r.MachineID(), Proc: proc})
	}
	events, err := r.Events(s.Adapter, s.ExternalID, now.Add(-24*time.Hour))
	if err != nil {
		return v, err
	}
	for _, event := range events {
		v.Events = append(v.Events, EventRow{MachineID: r.MachineID(), Event: event})
	}
	if s.Adapter == "claude" {
		v.Resume = "cd " + shellQuote(s.Cwd) + " && claude --resume " + shellQuote(s.ExternalID)
	}
	return v, nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

func projectRow(r Reader, repo model.Repo, ss []model.Session, now time.Time) (ProjectRow, []WorktreeRow, []HandoffRow, error) {
	row := ProjectRow{MachineID: r.MachineID()}
	wts, err := r.Worktrees(repo.ID)
	if err != nil {
		return row, nil, nil, err
	}
	hs, err := r.Handoffs(repo.ID, 0)
	if err != nil {
		return row, nil, nil, err
	}
	status := derive.Project(repo, wts, ss, hs, now)
	row.Status = ProjectStatus{Repo: status.Repo, Activity: status.Activity, AtRisk: nonNil(status.AtRisk), LiveAgents: status.LiveAgents, Where: []WherePart{}}
	for _, part := range status.Where {
		row.Status.Where = append(row.Status.Where, WherePart{MachineID: r.MachineID(), Source: part.Source, Text: part.Text, At: part.At})
	}
	summaries, err := r.Summaries(repo.ID)
	if err != nil {
		return row, nil, nil, err
	}
	if len(summaries) > 0 {
		s := summaries[0]
		row.Status.Where = []WherePart{{MachineID: r.MachineID(), Source: "claude", Text: s.Text, At: s.CreatedAt, Stale: s.StateHash != derive.SummaryStateHash(repo, wts, ss, hs, now, s.UsedSnippets)}}
	}
	row.Worktrees = len(wts)
	if len(wts) > 0 {
		row.Branch = wts[0].Branch
	}
	if len(hs) > 0 {
		row.LatestHandoff = hs[0].Text
	}
	for _, w := range wts {
		row.Dirty += w.Dirty + w.Untracked
		row.Unpushed += w.Ahead
	}
	worktrees := []WorktreeRow{}
	for _, wt := range wts {
		worktrees = append(worktrees, WorktreeRow{MachineID: r.MachineID(), Worktree: wt})
	}
	handoffs := []HandoffRow{}
	for _, h := range hs {
		handoffs = append(handoffs, HandoffRow{MachineID: r.MachineID(), CompletionEvent: h})
	}
	return row, worktrees, handoffs, nil
}

// Projects composes sourced project summaries and applies activity/risk filters.
func projectsLocal(r Reader, f Filter, now time.Time) (ProjectsView, error) {
	v := ProjectsView{Rows: []ProjectRow{}}
	repos, err := r.Repos()
	if err != nil {
		return v, err
	}
	ss, err := r.Sessions(store.SessionFilter{IncludeEnded: true, RepoPath: f.Project})
	if err != nil {
		return v, err
	}
	ss = visibleSessions(ss, f.All, now)
	for _, repo := range repos {
		if f.Project != "" && repo.Path != f.Project {
			continue
		}
		row, _, _, err := projectRow(r, repo, ss, now)
		if err != nil {
			return v, err
		}
		if f.AtRisk && len(row.Status.AtRisk) == 0 || f.Active && row.Status.Activity != "active" {
			continue
		}
		if !f.Since.IsZero() {
			recent := false
			for _, s := range ss {
				if s.RepoID != nil && *s.RepoID == repo.ID && !s.LastActivityAt.Before(f.Since) {
					recent = true
				}
			}
			for _, p := range row.Status.Where {
				if !p.At.Before(f.Since) {
					recent = true
				}
			}
			if !recent {
				continue
			}
		}
		v.Rows = append(v.Rows, row)
	}
	return v, nil
}

// Where reads a canonical repo's status, worktrees, agents and latest handoffs.
func whereLocal(r Reader, project string, now time.Time, all bool) (WhereView, error) {
	v := WhereView{WorktreeRows: []WorktreeRow{}, Handoffs: []HandoffRow{}}
	repos, err := r.Repos()
	if err != nil {
		return v, err
	}
	for _, repo := range repos {
		if repo.Path != project {
			continue
		}
		ss, err := r.Sessions(store.SessionFilter{IncludeEnded: true, RepoPath: project})
		if err != nil {
			return v, err
		}
		v.ProjectRow, v.WorktreeRows, v.Handoffs, err = projectRow(r, repo, visibleSessions(ss, all, now), now)
		if err != nil {
			return v, err
		}
		v.Agents, err = agentsLocal(r, AgentFilter{Project: project, All: all}, now)
		return v, err
	}
	return v, fmt.Errorf("unknown project %q", project)
}

func visibleSessions(ss []model.Session, all bool, now time.Time) []model.Session {
	result := []model.Session{}
	maxIdle := time.Hour
	if all {
		maxIdle = 24 * time.Hour
	}
	for _, s := range ss {
		if !(s.Hosted || strings.HasPrefix(s.Origin, "hosted")) || now.Sub(s.LastActivityAt) <= maxIdle {
			result = append(result, s)
		}
	}
	return result
}

// Inbox reads events; no acknowledgment or spool mutation occurs here.
func inboxLocal(r Reader, f Filter, now time.Time) (InboxView, error) {
	v := InboxView{Rows: []InboxRow{}}
	items, err := r.Inbox(f.Since, f.All)
	if err != nil {
		return v, err
	}
	repos, err := r.Repos()
	if err != nil {
		return v, err
	}
	ss, err := r.Sessions(store.SessionFilter{IncludeEnded: true})
	if err != nil {
		return v, err
	}
	byID := map[int64]model.Session{}
	for _, s := range ss {
		byID[s.ID] = s
	}
	for _, item := range items {
		if item.Kind == "worker_finished" {
			continue
		}
		if f.CaptureText != "" {
			item = inbox.Policy(item, f.CaptureText)
		}
		s := byID[item.SessionID]
		if f.Project != "" && !sessionProject(s, repos, f.Project) {
			continue
		}
		v.Rows = append(v.Rows, InboxRow{MachineID: r.MachineID(), InboxItem: item, Age: max(0, now.Sub(item.TS)), Waited: inbox.Waited(item, now), Project: projectName(s, repos), Adapter: s.Adapter, Title: s.Title, Branch: s.Branch})
	}
	return v, nil
}

func sessionProject(s model.Session, repos []model.Repo, path string) bool {
	for _, repo := range repos {
		if pathx.RecordedEqual(repo.Path, path) && s.RepoID != nil && *s.RepoID == repo.ID {
			return true
		}
	}
	return false
}

// Conflicts recomputes collisions using the current read time and stored edits.
func conflictsLocal(r Reader, f Filter, now time.Time) (ConflictsView, error) {
	v := ConflictsView{Current: []ConflictRow{}, Recent: []ConflictRow{}}
	ss, err := sessions(r, f, now)
	if err != nil {
		return v, err
	}
	files, err := r.SessionFiles(now.Add(-6 * time.Hour))
	if err != nil {
		return v, err
	}
	repos, err := r.Repos()
	if err != nil {
		return v, err
	}
	wts, err := r.Worktrees(0)
	if err != nil {
		return v, err
	}
	paths := map[int64]string{}
	for _, repo := range repos {
		paths[repo.ID] = repo.Path
	}
	sort.SliceStable(wts, func(i, j int) bool { return len(wts[i].Path) > len(wts[j].Path) })
	worktreeOf := func(path string) (string, string, string) {
		for _, w := range wts {
			if within(path, w.Path) {
				rel, _ := pathx.RecordedRel(w.Path, path)
				return paths[w.RepoID], w.Path, rel
			}
		}
		return "", "", ""
	}
	parents := map[int64]int64{}
	for _, s := range ss {
		if s.ParentID != nil {
			parents[s.ID] = *s.ParentID
		}
	}
	for _, c := range derive.Conflicts(ss, files, parents, worktreeOf, now) {
		row := ConflictRow{MachineID: r.MachineID(), Conflict: c}
		if c.Current {
			v.Current = append(v.Current, row)
		} else {
			v.Recent = append(v.Recent, row)
		}
	}
	return v, nil
}

// Orphans reads stored reclaim candidates, optionally within a repository.
func orphansLocal(r Reader, f Filter, now time.Time) (OrphansView, error) {
	v := OrphansView{Rows: []OrphanRow{}}
	os, err := r.Orphans()
	if err != nil {
		return v, err
	}
	wts := []model.Worktree{}
	if f.Project != "" {
		repos, err := r.Repos()
		if err != nil {
			return v, err
		}
		for _, repo := range repos {
			if pathx.RecordedEqual(repo.Path, f.Project) {
				wts, err = r.Worktrees(repo.ID)
				if err != nil {
					return v, err
				}
			}
		}
	}
	for _, o := range os {
		if f.Project != "" {
			match := within(o.Cwd, f.Project)
			for _, w := range wts {
				match = match || within(o.Cwd, w.Path)
			}
			if !match {
				continue
			}
		}
		if !f.Since.IsZero() && o.FirstSeen.Before(f.Since) {
			continue
		}
		o.Evidence = nonNil(o.Evidence)
		o.PIDs = nonNil(o.PIDs)
		v.Rows = append(v.Rows, OrphanRow{MachineID: r.MachineID(), Orphan: o, Age: max(0, now.Sub(o.StartTime))})
	}
	return v, nil
}

// machineLocal reads existing snapshots; app ranking never triggers a host scan.
func machineLocal(r Reader, f Filter, now time.Time) (MachineView, error) {
	v := MachineView{MachineID: r.MachineID(), Memory: []MemoryRow{}}
	sample, err := r.LatestSample()
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return v, err
	}
	v.Sample = sample
	if v.Sample.PressureLevel == "" {
		v.Sample.PressureLevel = "unknown"
	}
	procs, err := r.Procs()
	if err != nil {
		return v, err
	}
	ss, err := r.Sessions(store.SessionFilter{IncludeEnded: true})
	if err != nil {
		return v, err
	}
	repos, err := r.Repos()
	if err != nil {
		return v, err
	}
	worktrees, err := r.Worktrees(0)
	if err != nil {
		return v, err
	}
	ss = machineProjectSessions(ss, repos, worktrees)
	placements := derive.PlaceProcesses(procs, ss, nil)
	v.TopMemory, v.TopCPU = machineAppsPlaced(procs, placements)
	v.MCPServers = derive.MCPServers(procs, nil, now)
	buildAgentShare(&v, procs, ss, placements)
	trends := derive.NewMCPTrendBuilder()
	if err := r.ScanMCPMinutes(now, func(s model.MCPMinute) error { trends.Add(s); return nil }); err != nil {
		return v, err
	}
	derive.ApplyMCPTrends(&v.MCPServers, trends.Trends())
	allowed := map[int32]bool{}
	if f.Project != "" {
		ss, err := sessions(r, f, now)
		if err != nil {
			return v, err
		}
		idx := derive.NewProcIndex(procs)
		for _, s := range ss {
			for _, p := range procTree(s, idx) {
				allowed[p.PID] = true
			}
		}
	}
	totals := map[[2]string]uint64{}
	for _, p := range procs {
		if p.Agent == "" && p.Role == "" {
			continue
		}
		if f.Project != "" && !allowed[p.PID] {
			continue
		}
		totals[[2]string{p.Agent, p.Role}] += p.RSS
	}
	for key, rss := range totals {
		v.Memory = append(v.Memory, MemoryRow{MachineID: r.MachineID(), Agent: key[0], Role: key[1], RSS: rss})
	}
	sort.Slice(v.Memory, func(i, j int) bool {
		a, b := v.Memory[i], v.Memory[j]
		if a.RSS != b.RSS {
			return a.RSS > b.RSS
		}
		if a.Agent != b.Agent {
			return a.Agent < b.Agent
		}
		return a.Role < b.Role
	})
	return v, nil
}

// Summary combines live family counts, unacknowledged inbox and machine health.
func summaryLocal(r Reader, f Filter, now time.Time) (SummaryView, error) {
	v := SummaryView{MachineID: r.MachineID()}
	f.All = false
	agents, err := agentsLocal(r, f, now)
	if err != nil {
		return v, err
	}
	var count func([]AgentRow)
	count = func(rows []AgentRow) {
		for _, row := range rows {
			v.Agents++
			switch strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(row.Label, "~"), "?"), "?") {
			case "working":
				v.Working++
			case "waiting":
				v.Waiting++
			case "stalled":
				v.Stalled++
			}
			count(row.Children)
		}
	}
	count(agents.Rows)
	inbox, err := inboxLocal(r, f, now)
	if err != nil {
		return v, err
	}
	v.Inbox = len(inbox.Rows)
	machine, err := machineLocal(r, f, now)
	if err != nil {
		return v, err
	}
	v.Sample = machine.Sample
	orphans, err := orphansLocal(r, f, now)
	if err != nil {
		return v, err
	}
	v.Orphans = len(orphans.Rows)
	for _, o := range orphans.Rows {
		v.Reclaim += o.RSSTotal
	}
	return v, nil
}

func within(path, root string) bool { return pathx.RecordedWithin(path, root) }

// sessionAge is zero when the start is unknown (a transcript seen without
// registry metadata); renderers show that as "—" instead of a huge age.
func sessionAge(s model.Session, now time.Time) time.Duration {
	if s.StartedAt.IsZero() {
		return 0
	}
	return max(0, now.Sub(s.StartedAt))
}
