// Package view builds read-only, shared CLI and TUI presentation models.
package view

import (
	"time"

	"github.com/labstack/driga/internal/collector"
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
	SessionFiles(time.Time) (map[int64][]model.FileTouch, error)
	Events(string, string, time.Time) ([]model.Event, error)
	Inbox(time.Time, bool) ([]model.InboxItem, error)
	Orphans() ([]model.Orphan, error)
	LatestSample() (model.MachineSample, error)
	Sources() ([]model.SourceStatus, error)
	Gaps(time.Time) ([]model.Gap, error)
}

var _ Reader = (*store.Store)(nil)
var _ Reader = (*store.Tx)(nil)

// Intervals are the collector's current polling intervals.
type Intervals = collector.Intervals

// Fresh describes the age and last error of a collection source.
type Fresh struct {
	LastOKAt   time.Time `json:"last_ok_at"`
	AgeSeconds int       `json:"age_seconds"`
	Stale      bool      `json:"stale"`
	LastError  string    `json:"last_error"`
}

// Envelope is the versioned JSON contract shared by all read commands.
type Envelope struct {
	Schema       int              `json:"schema"`
	ObservedAt   time.Time        `json:"observed_at"`
	Mode         string           `json:"mode"`
	Freshness    map[string]Fresh `json:"freshness"`
	CoverageGaps []model.Gap      `json:"coverage_gaps"`
	Data         any              `json:"data"`
}

// NewEnvelope adds source freshness and the last day's coverage gaps to data.
func newEnvelopeLocal(r Reader, mode string, now time.Time, intervals Intervals, data any) (Envelope, error) {
	e := Envelope{Schema: 1, ObservedAt: now, Mode: mode, Freshness: map[string]Fresh{}, CoverageGaps: []model.Gap{}, Data: data}
	sources, err := r.Sources()
	if err != nil {
		return e, err
	}
	for _, s := range sources {
		interval := intervals.Procs
		switch s.Name {
		case "machine":
			interval = intervals.Machine
		case "git":
			interval = 5 * time.Minute
		}
		if interval <= 0 {
			interval = time.Minute
		}
		age := max(time.Duration(0), now.Sub(s.LastOKAt))
		e.Freshness[s.Name] = Fresh{LastOKAt: s.LastOKAt, AgeSeconds: int(age / time.Second), Stale: mode != "snapshot" && (s.LastOKAt.IsZero() || age > 3*interval), LastError: s.LastError}
	}
	gaps, err := r.Gaps(now.Add(-24 * time.Hour))
	if err != nil {
		return e, err
	}
	e.CoverageGaps = nonNil(gaps)
	return e, nil
}

// Filter scopes a view to a canonical repo path and an optional time window.
type Filter struct {
	Scope   store.Scope
	Project string
	Since   time.Time
	All     bool
	AtRisk  bool
	Active  bool
}

// AgentFilter selects live sessions, or also those ended in the last day.
type AgentFilter = Filter

// AgentRow describes one agent and its nested family.
type AgentRow struct {
	MachineFresh
	MachineID        string        `json:"machine_id"`
	ID               int64         `json:"id"`
	Label            string        `json:"label"`
	Adapter          string        `json:"adapter"`
	Origin           string        `json:"origin"`
	Project          string        `json:"project"`
	Branch           string        `json:"branch"`
	Title            string        `json:"title"`
	Age              time.Duration `json:"age"`
	Mem              uint64        `json:"mem"`
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
	MachineID string    `json:"machine_id"`
	Source    string    `json:"source"`
	Text      string    `json:"text"`
	At        time.Time `json:"at"`
}

// ProjectRow is a derived repository status with checkout totals.
type ProjectRow struct {
	MachineFresh
	MachineID string        `json:"machine_id"`
	Status    ProjectStatus `json:"status"`
	Dirty     int           `json:"dirty"`
	Unpushed  int           `json:"unpushed"`
	Worktrees int           `json:"worktrees"`
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
	MachineFresh
	MachineID string `json:"machine_id"`
	model.InboxItem
	Age     time.Duration `json:"age"`
	Project string        `json:"project"`
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
	Age time.Duration `json:"age"`
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
	RSS       uint64 `json:"rss"`
}

// MachineView contains the most recent sample and top agent memory users.
type MachineView struct {
	MachineFresh
	Machines  []MachineView       `json:"machines,omitempty"`
	MachineID string              `json:"machine_id"`
	Sample    model.MachineSample `json:"sample"`
	Memory    []MemoryRow         `json:"memory"`
}

// SummaryView contains the counts and health values used by the summary line.
type SummaryView struct {
	MachineID string              `json:"machine_id"`
	Agents    int                 `json:"agents"`
	Working   int                 `json:"working"`
	Waiting   int                 `json:"waiting"`
	Stalled   int                 `json:"stalled"`
	Inbox     int                 `json:"inbox"`
	Sample    model.MachineSample `json:"sample"`
	Orphans   int                 `json:"orphans"`
	Reclaim   uint64              `json:"reclaim"`
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
	LastSeen    time.Time     `json:"last_seen"`
	LastSeenAge time.Duration `json:"last_seen_age"`
}

// MachineRow lists a known machine, cached counts, and clock health.
type MachineRow struct {
	model.Machine
	Agents      int           `json:"agents"`
	Inbox       int           `json:"inbox"`
	LastSeenAge time.Duration `json:"last_seen_age"`
	SkewWarning string        `json:"skew_warning,omitempty"`
}

// MachinesView lists all known machines, including peers with stale caches.
type MachinesView struct {
	Rows    []MachineRow `json:"rows"`
	Warning string       `json:"warning,omitempty"`
}
