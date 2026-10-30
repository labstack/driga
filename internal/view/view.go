// Package view builds shared presentation models for CLI, TUI, web and MCP consumers.
package view

import (
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

// Reader is the store's read API used by presentation builders.
type Reader interface {
	MachineID() string
	Machines() ([]model.Machine, error)
	ReadMachine(string) *store.MachineReader
	Sessions(store.SessionFilter) ([]model.Session, error)
	Session(int64) (model.Session, error)
	Procs() ([]model.Proc, error)
	Repos() ([]model.Repo, error)
	Worktrees(int64) ([]model.Worktree, error)
	Handoffs(int64, int) ([]model.CompletionEvent, error)
	SessionHandoffs(int64, time.Time, time.Time) ([]model.CompletionEvent, error)
	Summaries(int64) ([]model.Summary, error)
	SessionFiles(time.Time) (map[int64][]model.FileTouch, error)
	PanelEvents(string, string, time.Time) ([]model.Event, error)
	AllPanelEvents(time.Time) ([]model.Event, error)
	Events(string, string, time.Time) ([]model.Event, error)
	RecentEvents(time.Time, int) ([]model.Event, error)
	Inbox(time.Time, bool) ([]model.InboxItem, error)
	Orphans() ([]model.Orphan, error)
	LatestSample() (model.MachineSample, error)
	Samples(time.Time) ([]model.MachineSample, error)
	PromptIntervals(time.Time) ([]store.PromptInterval, error)
	PanelPolls(time.Time) ([]store.PanelPoll, error)
	EventCounts(time.Time, time.Time) ([]store.EventCount, error)
	BoardEvidence(time.Time, time.Time) ([]model.Event, map[int64][]store.ObservationMinute, error)
	ObservationMinutes(int64, time.Time) ([]store.ObservationMinute, error)
	ScanMCPHistory(time.Time, time.Time, func(model.MCPObservation) error) error
	Sources() ([]model.SourceStatus, error)
	Gaps(time.Time) ([]model.Gap, error)
}

var _ Reader = (*store.Store)(nil)
var _ Reader = (*store.Tx)(nil)

// Intervals are the collector's current polling intervals.
type Intervals = collector.Intervals

// Fresh describes the age and last error of a collection source.
type Fresh struct {
	LastOKAt  time.Time `json:"last_ok_at"`
	AgeMS     int64     `json:"age_ms"`
	Stale     bool      `json:"stale"`
	LastError string    `json:"last_error"`
	Status    string    `json:"status,omitempty"`
}

// Envelope is the versioned JSON contract shared by all read commands.
type ErrorRecord struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Envelope struct {
	Error        *ErrorRecord     `json:"error,omitzero"`
	Truncated    *bool            `json:"truncated,omitzero"`
	Schema       int              `json:"schema"`
	ObservedAt   time.Time        `json:"observed_at"`
	Mode         string           `json:"mode"`
	Freshness    map[string]Fresh `json:"freshness"`
	CoverageGaps []model.Gap      `json:"coverage_gaps"`
	Data         any              `json:"data"`
}

// NewEnvelope adds source freshness and the last day's coverage gaps to data.
func newEnvelopeLocal(r Reader, mode string, now time.Time, intervals Intervals, data any) (Envelope, error) {
	e := Envelope{Schema: 2, ObservedAt: now, Mode: mode, Freshness: map[string]Fresh{}, CoverageGaps: []model.Gap{}, Data: data}
	sources, err := r.Sources()
	if err != nil {
		return e, err
	}
	for _, s := range sources {
		e.Freshness[s.Name] = SourceFreshness(s, mode, now, intervals)
	}
	gaps, err := r.Gaps(now.Add(-24 * time.Hour))
	if err != nil {
		return e, err
	}
	e.CoverageGaps = nonNil(gaps)
	return e, nil
}

// SourceFreshness defines polling deadlines and event-driven source health for
// every view and diagnostic. Quiet event sources have no polling deadline.
func SourceFreshness(s model.SourceStatus, mode string, now time.Time, intervals Intervals) Fresh {
	interval := SourceInterval(s.Name, intervals)
	age := max(time.Duration(0), now.Sub(s.LastOKAt))
	if s.LastOKAt.IsZero() {
		age = 0
	}
	stale := s.LastOKAt.IsZero() || mode != "snapshot" && age > 3*interval
	status := ""
	if s.Name == "otlp" || s.Name == "sockets" {
		stale = s.LastError != ""
		status = "last_event"
		if s.LastOKAt.IsZero() && !stale {
			status = "not_yet_receiving"
		}
	}
	return Fresh{LastOKAt: s.LastOKAt, AgeMS: age.Milliseconds(), Stale: stale, LastError: s.LastError, Status: status}
}

// Filter scopes a view to a canonical repo path and an optional time window.
type Filter struct {
	Full        bool
	FlowMinutes int
	CaptureText string
	Scope       store.Scope
	Project     string
	Since       time.Time
	All         bool
	AtRisk      bool
	Active      bool
}

// AgentFilter selects live sessions, or also those ended in the last day.
type AgentFilter = Filter

// AgentRow describes one agent and its nested family.
type AgentRow struct {
	StartedFrom string `json:"started_from,omitempty"`
	MachineFresh
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
}

// AgentsView groups agents into supervisor families.
type AgentsView struct {
	Rows []AgentRow `json:"rows"`
}

// AgentView contains the stored state and recent evidence for one session.
type AgentView struct {
	MachineFresh
	MachineID  string        `json:"machine_id"`
	Session    model.Session `json:"session"`
	Heuristic  bool          `json:"heuristic"`
	Label      string        `json:"label"`
	ObservedAt time.Time     `json:"observed_at"`
	Files      []FileRow     `json:"files"`
	Procs      []ProcRow     `json:"procs"`
	Events     []EventRow    `json:"events"`
	Resume     string        `json:"resume,omitempty"`
}

// FileRow identifies the machine on which a file edit was observed.
type FileRow struct {
	MachineID string `json:"machine_id"`
	model.FileTouch
}

// ProcRow identifies the machine on which a process was sampled.
type ProcRow struct {
	MachineID string `json:"machine_id"`
	model.Proc
}

// EventRow identifies the machine on which an event occurred.
type EventRow struct {
	MachineID string `json:"machine_id"`
	model.Event
}

// WorktreeRow identifies the machine hosting a checkout.
type WorktreeRow struct {
	MachineID string `json:"machine_id"`
	model.Worktree
}

// HandoffRow identifies the machine on which a turn finished.
type HandoffRow struct {
	MachineID string `json:"machine_id"`
	model.CompletionEvent
}

// ProjectStatus is a repository's derived activity, risks and sourced summary.
type ProjectStatus struct {
	Repo       model.Repo  `json:"repo"`
	Activity   string      `json:"activity"`
	AtRisk     []string    `json:"at_risk"`
	LiveAgents int         `json:"live_agents"`
	Where      []WherePart `json:"where"`
}

// WherePart preserves the source and timestamp of one project summary part.
type WherePart struct {
	Stale     bool      `json:"stale,omitzero"`
	MachineID string    `json:"machine_id"`
	Source    string    `json:"source"`
	Text      string    `json:"text"`
	At        time.Time `json:"occurred_at"`
}

// ProjectRow is a derived repository status with checkout totals.
type ProjectRow struct {
	MachineFresh
	MachineID     string        `json:"machine_id"`
	Status        ProjectStatus `json:"status"`
	Dirty         int           `json:"dirty"`
	Unpushed      int           `json:"unpushed"`
	Worktrees     int           `json:"worktrees"`
	Branch        string        `json:"branch"`
	LatestHandoff string        `json:"latest_handoff,omitempty"`
}

// ProjectsView lists repositories and their sourced status.
type ProjectsView struct {
	Rows []ProjectRow `json:"rows"`
}

// WhereView combines a project's status, checkouts, families and handoffs.
type WhereView struct {
	ProjectRow
	WorktreeRows []WorktreeRow `json:"worktrees_detail"`
	Agents       AgentsView    `json:"agents"`
	Handoffs     []HandoffRow  `json:"handoffs"`
}

// InboxRow is an event with its project and age.
type InboxRow struct {
	Historical bool `json:"historical,omitzero"`
	MachineFresh
	MachineID string `json:"machine_id"`
	model.InboxItem
	Age     time.Duration `json:"age_ms"`
	Waited  time.Duration `json:"waited_ms"`
	Project string        `json:"project"`
	Adapter string        `json:"adapter"`
	Title   string        `json:"title"`
	Branch  string        `json:"branch"`
	// Checkout and Files are what a finished turn left to review: the
	// session's worktree as last scanned, and the files it edited.
	Checkout *model.Worktree `json:"checkout,omitzero"`
	Files    []string        `json:"files,omitempty"`
}

// InboxView lists attention and completion events without acknowledging them.
type InboxView struct {
	Rows []InboxRow `json:"rows"`
}

// ConflictRow is an overlap on the current machine.
type ConflictRow struct {
	MachineFresh
	MachineID string `json:"machine_id"`
	model.Conflict
}

// ConflictsView separates current collisions from historical overlap.
type ConflictsView struct {
	Current []ConflictRow `json:"current"`
	Recent  []ConflictRow `json:"recent"`
}

// OrphanRow adds age and machine identity to an orphan candidate.
type OrphanRow struct {
	MachineFresh
	MachineID string `json:"machine_id"`
	model.Orphan
	Age time.Duration `json:"age_ms"`
}

// OrphansView lists reclaim candidates.
type OrphansView struct {
	Rows []OrphanRow `json:"rows"`
}

// MemoryRow aggregates process RSS by agent and role.
type MemoryRow struct {
	MachineFresh
	MachineID string `json:"machine_id"`
	Agent     string `json:"agent"`
	Role      string `json:"role"`
	RSS       uint64 `json:"rss_bytes"`
}

// MachineView contains host totals, whole-machine app rankings, and agent role totals.
type MachineView struct {
	Full       bool      `json:"full,omitzero"`
	Truncated  bool      `json:"truncated,omitzero"`
	TrendSince time.Time `json:"trend_since,omitzero"`
	// Machines serves human fleet views; the wire includes only additional hosts.
	AdditionalMachines []MachineView `json:"machines,omitempty"`
	AgentShare         AgentShare    `json:"agent_share"`
	SessionCosts       []SessionCost `json:"session_costs"`
	AgentNoSession     []MachineApp  `json:"agent_processes_without_session"`
	LikelyAgent        []MachineApp  `json:"likely_agent_work"`
	OtherApps          MachineApp    `json:"other_apps"`
	MachineFresh
	MCPServers derive.MCPServerView `json:"mcp_servers"`
	TopMemory  []MachineApp         `json:"top_memory"`
	TopCPU     []MachineApp         `json:"top_cpu"`
	Machines   []MachineView        `json:"-"`
	MachineID  string               `json:"machine_id"`
	Sample     model.MachineSample  `json:"sample"`
	Memory     []MemoryRow          `json:"memory"`
}

// SummaryView contains the counts and health values used by the summary line.
type SummaryView struct {
	MachineID string              `json:"machine_id"`
	Agents    int                 `json:"agents"`
	Working   int                 `json:"working"`
	Waiting   int                 `json:"waiting"`
	Stuck     int                 `json:"stuck"`
	Inbox     int                 `json:"inbox"`
	Sample    model.MachineSample `json:"sample"`
	Orphans   int                 `json:"orphans"`
	Reclaim   uint64              `json:"reclaim_bytes"` // Legacy field: orphan process RSS, not disk cleanup.
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// MachineFresh records the origin and connection freshness of a presentation row.
type MachineFresh struct {
	MachineName string        `json:"machine_name"`
	State       string        `json:"machine_state"`
	Stale       bool          `json:"stale"`
	LastSeen    time.Time     `json:"last_seen_at"`
	LastSeenAge time.Duration `json:"last_seen_age_ms"`
}

// MachineRow lists a known machine, cached counts, and clock health.
type MachineRow struct {
	Sample *model.MachineSample `json:"sample"`
	model.Machine
	Agents      int           `json:"agents"`
	Inbox       int           `json:"inbox"`
	LastSeenAge time.Duration `json:"last_seen_age_ms"`
	SkewWarning string        `json:"skew_warning,omitempty"`
}

// MachinesView lists all known machines, including peers with stale caches.
type MachinesView struct {
	Rows    []MachineRow `json:"rows"`
	Warning string       `json:"warning,omitempty"`
}

// SourceInterval matches independently scheduled collector sources. Git's
// full scan can take five minutes even while viewers request faster refreshes.
func SourceInterval(name string, intervals Intervals) time.Duration {
	interval := intervals.Procs
	switch name {
	case "machine":
		interval = intervals.Machine
	case "cwds":
		interval = intervals.Cwds
	case "git":
		interval = 5 * time.Minute
	}
	if interval <= 0 {
		interval = time.Minute
	}
	return interval
}
