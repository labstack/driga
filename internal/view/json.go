package view

import (
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/jsonx"
	"github.com/labstack/driga/internal/model"
)

func (v TailEvent) MarshalJSON() ([]byte, error) {
	type plain TailEvent
	return jsonx.Marshal(plain(v))
}
func (v ActivityBucket) MarshalJSON() ([]byte, error) {
	type plain ActivityBucket
	return jsonx.Marshal(plain(v))
}
func (v ActivityMachine) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.MachineFresh, struct {
		MachineID string           `json:"machine_id"`
		Buckets   []ActivityBucket `json:"buckets"`
	}{v.MachineID, v.Buckets})
}
func (v ActivityView) MarshalJSON() ([]byte, error) {
	type plain ActivityView
	return jsonx.Marshal(plain(v))
}
func (v Fresh) MarshalJSON() ([]byte, error)    { type plain Fresh; return jsonx.Marshal(plain(v)) }
func (v Envelope) MarshalJSON() ([]byte, error) { type plain Envelope; return jsonx.Marshal(plain(v)) }
func (v AgentRow) MarshalJSON() ([]byte, error) {
	v.Label, v.Heuristic = jsonx.Label(v.Label, v.Heuristic)
	return jsonx.Object(v.MachineFresh, struct {
		StartedFrom      string        `json:"started_from,omitempty"`
		MachineID        string        `json:"machine_id"`
		ID               int64         `json:"id"`
		RepoID           int64         `json:"repo_id"`
		LastActivityAt   time.Time     `json:"last_activity_at"`
		TitleFromPrompt  bool          `json:"title_from_prompt,omitzero"`
		Heuristic        bool          `json:"heuristic"`
		Label            string        `json:"label"`
		Adapter          string        `json:"adapter"`
		Origin           string        `json:"origin"`
		Project          string        `json:"project"`
		Branch           string        `json:"branch"`
		Title            string        `json:"title"`
		Age              time.Duration `json:"age_ms"`
		Mem              uint64        `json:"mem_bytes"`
		Evidence         string        `json:"evidence"`
		ObservedAt       time.Time     `json:"observed_at"`
		FinishedChildren int           `json:"finished_children"`
		Children         []AgentRow    `json:"children"`
	}{v.StartedFrom, v.MachineID, v.ID, v.RepoID, v.LastActivityAt, v.TitleFromPrompt, v.Heuristic, v.Label, v.Adapter, v.Origin, v.Project, v.Branch, v.Title, v.Age, v.Mem, v.Evidence, v.ObservedAt, v.FinishedChildren, v.Children})
}
func (v AgentsView) MarshalJSON() ([]byte, error) {
	type plain AgentsView
	return jsonx.Marshal(plain(v))
}
func (v AgentView) MarshalJSON() ([]byte, error) {
	v.Label, v.Heuristic = jsonx.Label(v.Label, v.Heuristic)
	return jsonx.Object(v.MachineFresh, struct {
		MachineID  string        `json:"machine_id"`
		Session    model.Session `json:"session"`
		Heuristic  bool          `json:"heuristic"`
		Label      string        `json:"label"`
		ObservedAt time.Time     `json:"observed_at"`
		Files      []FileRow     `json:"files"`
		Procs      []ProcRow     `json:"procs"`
		Events     []EventRow    `json:"events"`
		Resume     string        `json:"resume,omitempty"`
	}{v.MachineID, v.Session, v.Heuristic, v.Label, v.ObservedAt, v.Files, v.Procs, v.Events, v.Resume})
}
func (v FileRow) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.FileTouch, struct {
		MachineID string `json:"machine_id"`
	}{v.MachineID})
}
func (v ProcRow) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.Proc, struct {
		MachineID string `json:"machine_id"`
	}{v.MachineID})
}
func (v EventRow) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.Event, struct {
		MachineID string `json:"machine_id"`
	}{v.MachineID})
}
func (v WorktreeRow) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.Worktree, struct {
		MachineID string `json:"machine_id"`
	}{v.MachineID})
}
func (v HandoffRow) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.CompletionEvent, struct {
		MachineID string `json:"machine_id"`
	}{v.MachineID})
}
func (v ProjectStatus) MarshalJSON() ([]byte, error) {
	type plain ProjectStatus
	return jsonx.Marshal(plain(v))
}
func (v WherePart) MarshalJSON() ([]byte, error) {
	type plain WherePart
	return jsonx.Marshal(plain(v))
}
func (v ProjectRow) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.MachineFresh, struct {
		MachineID     string        `json:"machine_id"`
		Status        ProjectStatus `json:"status"`
		Dirty         int           `json:"dirty"`
		Unpushed      int           `json:"unpushed"`
		Worktrees     int           `json:"worktrees"`
		Branch        string        `json:"branch"`
		LatestHandoff string        `json:"latest_handoff,omitempty"`
	}{v.MachineID, v.Status, v.Dirty, v.Unpushed, v.Worktrees, v.Branch, v.LatestHandoff})
}
func (v ProjectsView) MarshalJSON() ([]byte, error) {
	type plain ProjectsView
	return jsonx.Marshal(plain(v))
}
func (v WhereView) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.ProjectRow, struct {
		WorktreeRows []WorktreeRow `json:"worktrees_detail"`
		Agents       AgentsView    `json:"agents"`
		Handoffs     []HandoffRow  `json:"handoffs"`
	}{v.WorktreeRows, v.Agents, v.Handoffs})
}
func (v InboxRow) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.MachineFresh, v.InboxItem, struct {
		Historical bool          `json:"historical,omitzero"`
		MachineID  string        `json:"machine_id"`
		Age        time.Duration `json:"age_ms"`
		Waited     time.Duration `json:"waited_ms"`
		Project    string        `json:"project"`
		Adapter    string        `json:"adapter"`
		Title      string        `json:"title"`
		Branch     string        `json:"branch"`
	}{v.Historical, v.MachineID, v.Age, v.Waited, v.Project, v.Adapter, v.Title, v.Branch})
}
func (v InboxView) MarshalJSON() ([]byte, error) {
	type plain InboxView
	return jsonx.Marshal(plain(v))
}
func (v ConflictRow) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.MachineFresh, v.Conflict, struct {
		MachineID string `json:"machine_id"`
	}{v.MachineID})
}
func (v ConflictsView) MarshalJSON() ([]byte, error) {
	type plain ConflictsView
	return jsonx.Marshal(plain(v))
}
func (v OrphanRow) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.MachineFresh, v.Orphan, struct {
		MachineID string        `json:"machine_id"`
		Age       time.Duration `json:"age_ms"`
	}{v.MachineID, v.Age})
}
func (v OrphansView) MarshalJSON() ([]byte, error) {
	type plain OrphansView
	return jsonx.Marshal(plain(v))
}
func (v MemoryRow) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.MachineFresh, struct {
		MachineID string `json:"machine_id"`
		Agent     string `json:"agent"`
		Role      string `json:"role"`
		RSS       uint64 `json:"rss_bytes"`
	}{v.MachineID, v.Agent, v.Role, v.RSS})
}
func (v MachineView) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.MachineFresh, struct {
		Full               bool                 `json:"full,omitzero"`
		Truncated          bool                 `json:"truncated,omitzero"`
		TrendSince         time.Time            `json:"trend_since,omitzero"`
		AdditionalMachines []MachineView        `json:"machines,omitempty"`
		AgentShare         AgentShare           `json:"agent_share"`
		SessionCosts       []SessionCost        `json:"session_costs"`
		AgentNoSession     []MachineApp         `json:"agent_processes_without_session"`
		LikelyAgent        []MachineApp         `json:"likely_agent_work"`
		OtherApps          MachineApp           `json:"other_apps"`
		MCPServers         derive.MCPServerView `json:"mcp_servers"`
		TopMemory          []MachineApp         `json:"top_memory"`
		TopCPU             []MachineApp         `json:"top_cpu"`
		Machines           []MachineView        `json:"-"`
		MachineID          string               `json:"machine_id"`
		Sample             model.MachineSample  `json:"sample"`
		Memory             []MemoryRow          `json:"memory"`
	}{v.Full, v.Truncated, v.TrendSince, v.AdditionalMachines, v.AgentShare, v.SessionCosts, v.AgentNoSession, v.LikelyAgent, v.OtherApps, v.MCPServers, v.TopMemory, v.TopCPU, v.Machines, v.MachineID, v.Sample, v.Memory})
}
func (v SummaryView) MarshalJSON() ([]byte, error) {
	type plain SummaryView
	return jsonx.Marshal(plain(v))
}
func (v MachineFresh) MarshalJSON() ([]byte, error) {
	type plain MachineFresh
	return jsonx.Marshal(plain(v))
}
func (v MachineRow) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.Machine, struct {
		Agents      int           `json:"agents"`
		Inbox       int           `json:"inbox"`
		LastSeenAge time.Duration `json:"last_seen_age_ms"`
		SkewWarning string        `json:"skew_warning,omitempty"`
	}{v.Agents, v.Inbox, v.LastSeenAge, v.SkewWarning})
}
func (v MachinesView) MarshalJSON() ([]byte, error) {
	type plain MachinesView
	return jsonx.Marshal(plain(v))
}

func (v MachineProcess) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.Proc, struct {
		Memory      uint64              `json:"memory_bytes"`
		Attribution *MachineAttribution `json:"attribution,omitzero"`
	}{v.Memory, v.Attribution})
}
