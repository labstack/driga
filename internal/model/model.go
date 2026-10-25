// Package model holds plain data types shared across driga's packages.
// It has no logic and no dependencies beyond the standard library.
package model

import "time"

// Machine is the host the collector runs on.
type Machine struct {
	Name          string    `json:"name"`
	DeviceID      string    `json:"device_id"`
	Addr          string    `json:"addr"`
	DrigaVersion  string    `json:"driga_version"`
	FeedEpoch     string    `json:"feed_epoch"`
	FeedSeq       int64     `json:"feed_seq"`
	LastContactAt time.Time `json:"last_contact_at"`
	LastError     string    `json:"last_error"`
	ClockSkewMS   int64     `json:"clock_skew_ms"`
	State         string    `json:"state"`
	MachineID     string    `json:"machine_id"`
	ID            string    `json:"id"`
	Hostname      string    `json:"hostname"`
	OS            string    `json:"os"`
	Arch          string    `json:"arch"`
	Cores         int       `json:"cores"`
	MemTotal      uint64    `json:"mem_total_bytes"`
	FirstSeen     time.Time `json:"first_seen_at"`
	LastSeen      time.Time `json:"last_seen_at"`
}

// ChatPreamble opens every prompt Driga's Chat sends to claude, so the
// collector can tell Chat's own claude sessions from the person's agents.
const ChatPreamble = "You are driga Chat."

// Proc is one process in a snapshot.
type Proc struct {
	// Partial means one or more live Windows process queries were denied.
	Partial       bool      `json:"partial,omitzero"`
	TTY           string    `json:"tty,omitempty"`
	MachineID     string    `json:"machine_id"`
	PID           int32     `json:"pid"`
	PPID          int32     `json:"ppid"`
	StartTime     time.Time `json:"started_at"`
	Footprint     uint64    `json:"footprint_bytes"`
	Swap          uint64    `json:"swap_bytes"`
	MemoryMeasure string    `json:"memory_measure"`
	RSS           uint64    `json:"rss_bytes"`
	CPUSeconds    float64   `json:"cpu_ms"`
	CPUPercent    float64   `json:"cpu_percent"`
	Cmdline       string    `json:"cmdline"`
	Argv          []string  `json:"argv"`
	Exe           string    `json:"exe"`
	Cwd           string    `json:"cwd"`
	Role          string    `json:"role,omitempty"`
	Agent         string    `json:"agent,omitempty"`
	SessionID     int64     `json:"session_id,omitzero"`
	// Zombie is live inspection data; cleanup never uses stored process state.
	Zombie bool `json:"-"`
	// OwnerUID is the real uid when OwnerKnown; live inspection only, never
	// stored or shared.
	OwnerUID   uint32 `json:"-"`
	OwnerKnown bool   `json:"-"`
}

// MachineSample is one machine-health reading.
type MachineSample struct {
	// Windows commit accounting is not pagefile occupancy.
	CommitLimit uint64 `json:"commit_limit_bytes,omitzero"`
	CommitUsed  uint64 `json:"commit_used_bytes,omitzero"`
	// DiskTotal is live inspection data for the collector's low_disk threshold.
	// It is not a new persisted column.
	DiskTotal     uint64    `json:"-"`
	MachineID     string    `json:"machine_id"`
	TS            time.Time `json:"sampled_at"`
	MemUsed       uint64    `json:"mem_used_bytes"`
	MemCompressed uint64    `json:"mem_compressed_bytes"`
	MemTotal      uint64    `json:"mem_total_bytes"`
	SwapUsed      uint64    `json:"swap_used_bytes"`
	SwapTotal     uint64    `json:"swap_total_bytes"`
	DiskFree      uint64    `json:"disk_free_bytes"`
	PressureLevel string    `json:"pressure_level"`
	PressureFree  float64   `json:"pressure_free"`
	// Negative load values mean unavailable (Windows); zero is a real reading.
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
	Cores  int     `json:"cores"`
	// PsiMemSome10 is Linux PSI memory "some avg10"; nil when unavailable.
	PsiMemSome10 *float64 `json:"psi_mem_some10,omitzero"`
}

// Repo is a git repository (the main worktree's path).
type Repo struct {
	MachineID string `json:"machine_id"`
	ID        int64  `json:"id"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	RemoteURL string `json:"remote_url,omitempty"`
}

// Worktree is one checkout of a repo.
type Worktree struct {
	MachineID         string    `json:"machine_id"`
	ID                int64     `json:"id"`
	RepoID            int64     `json:"repo_id"`
	Path              string    `json:"path"`
	Branch            string    `json:"branch"`
	Head              string    `json:"head"`
	Ahead             int       `json:"ahead"`
	Behind            int       `json:"behind"`
	Dirty             int       `json:"dirty"`
	Untracked         int       `json:"untracked"`
	OldestDirtyMtime  time.Time `json:"oldest_dirty_modified_at"`
	LastCommitAt      time.Time `json:"last_commit_at"`
	LastCommitSubject string    `json:"last_commit_subject"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// SessionObs is what an adapter observed about one agent session.
type SessionObs struct {
	ParentExternalID  string            `json:"parent_external_id,omitempty"`
	Adapter           string            `json:"adapter"`
	ExternalID        string            `json:"external_id"`
	Origin            string            `json:"origin"`
	Version           string            `json:"version,omitempty"`
	PID               int32             `json:"pid"`
	ProcStart         time.Time         `json:"proc_started_at"`
	Cwd               string            `json:"cwd"`
	Branch            string            `json:"branch,omitempty"`
	Title             string            `json:"title,omitempty"`
	LastPrompt        string            `json:"last_prompt,omitempty"`
	RegistryUpdatedAt time.Time         `json:"registry_updated_at,omitzero"`
	RegistryStatus    string            `json:"registry_status,omitempty"`
	StartedAt         time.Time         `json:"started_at"`
	LastActivityAt    time.Time         `json:"last_activity_at"`
	LastPromptAt      time.Time         `json:"last_prompt_at"`
	Hosted            bool              `json:"hosted"`
	Completions       []CompletionEvent `json:"completions,omitempty"`
	FilesTouched      []FileTouch       `json:"files_touched,omitempty"`
	TurnOpen          bool              `json:"turn_open"`
	SourcePath        string            `json:"source_path,omitempty"`
}

// CompletionEvent is one finished turn.
type CompletionEvent struct {
	MachineID string    `json:"machine_id"`
	ID        string    `json:"id"`
	TS        time.Time `json:"occurred_at"`
	Text      string    `json:"text,omitempty"`
}

// FileTouch is one file edited by a session.
type FileTouch struct {
	MachineID string    `json:"machine_id"`
	Path      string    `json:"path"`
	TS        time.Time `json:"occurred_at"`
}

// HookEvent is one line in the spool. Event retains the adapter's protocol name;
// ack uses Message as the inbox item id, and summary carries Summary instead of text.
type HookEvent struct {
	Recoverable      bool      `json:"recoverable,omitzero"`
	Origin           string    `json:"origin,omitempty"`
	PID              int32     `json:"pid,omitzero"`
	ProcStart        time.Time `json:"proc_started_at,omitzero"`
	Title            string    `json:"title,omitempty"`
	Prompt           string    `json:"prompt,omitempty"`
	Source           string    `json:"source,omitempty"`
	Reason           string    `json:"reason,omitempty"`
	NotificationType string    `json:"notification_type,omitempty"`
	Tool             string    `json:"tool,omitempty"`
	Detail           string    `json:"detail,omitempty"`
	Summary          *Summary  `json:"summary,omitzero"`
	MachineID        string    `json:"machine_id,omitempty"`
	TS               time.Time `json:"occurred_at"`
	Adapter          string    `json:"adapter"`
	SessionID        string    `json:"session_id"`
	Cwd              string    `json:"cwd,omitempty"`
	Event            string    `json:"event"`
	Message          string    `json:"message,omitempty"`
}

// Summary records the inputs' digest and provenance, never the input text.
type Summary struct {
	MachineID    string    `json:"machine_id"`
	ProjectID    int64     `json:"project_id"`
	InputHash    string    `json:"input_hash"`
	StateHash    string    `json:"state_hash"`
	Text         string    `json:"text"`
	Model        string    `json:"model"`
	UsedSnippets bool      `json:"used_snippets"`
	CreatedAt    time.Time `json:"created_at"`
}

// Session is a stored session with derived fields.
type Session struct {
	StartedFrom string      `json:"started_from,omitempty"`
	Mixer       *StateMixer `json:"mixer,omitzero"`
	MachineID   string      `json:"machine_id"`
	ID          int64       `json:"id"`
	ParentID    *int64      `json:"parent_id,omitzero"`
	SessionObs
	RepoID     *int64     `json:"repo_id,omitzero"`
	WorktreeID *int64     `json:"worktree_id,omitzero"`
	Label      string     `json:"label"`
	Heuristic  bool       `json:"heuristic"`
	Evidence   string     `json:"evidence"`
	EndedAt    *time.Time `json:"ended_at,omitzero"`
}

// InboxItem is one event needing the user's attention.
type InboxItem struct {
	Reason         string     `json:"reason,omitempty"`
	Tool           string     `json:"tool,omitempty"`
	Detail         string     `json:"detail,omitempty"`
	WaitingSinceAt time.Time  `json:"waiting_since_at,omitzero"`
	Source         string     `json:"source,omitempty"`
	EndedAt        *time.Time `json:"ended_at,omitzero"`
	Jump           *InboxJump `json:"jump,omitzero"`
	AckPendingAt   *time.Time `json:"ack_pending_at,omitzero"`
	MachineID      string     `json:"machine_id"`
	ID             string     `json:"id"`
	SessionID      int64      `json:"session_id"`
	Kind           string     `json:"kind"` // finished|waiting
	TS             time.Time  `json:"occurred_at"`
	Summary        string     `json:"summary"`
	AckedAt        *time.Time `json:"acked_at,omitzero"`
}

// InboxJump contains only a proven destination or a copyable resume command.
type InboxJump struct {
	App     string `json:"app,omitempty"`
	Kind    string `json:"kind"`
	Command string `json:"command,omitempty"`
	Target  string `json:"target,omitempty"`
	URL     string `json:"url,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	Server  string `json:"server,omitempty"`
}

// Orphan is a process tree left behind by a dead agent session.
type Orphan struct {
	MachineID string    `json:"machine_id"`
	ID        string    `json:"id"` // root_pid:start_unix
	RootPID   int32     `json:"root_pid"`
	StartTime time.Time `json:"started_at"`
	Signature string    `json:"signature"`
	ProcCount int       `json:"proc_count"`
	RSSTotal  uint64    `json:"rss_total_bytes"`
	Cwd       string    `json:"cwd"`
	Young     bool      `json:"young"`
	Verified  bool      `json:"verified"` // idle with no external peers on consecutive passes
	Evidence  []string  `json:"evidence"`
	FirstSeen time.Time `json:"first_seen_at"`
	PIDs      []int32   `json:"pids"`
	// Windows pins every member because creator PIDs survive parent exit.
	ProcessStarts map[int32]time.Time `json:"process_starts,omitempty"`
}

// Conflict is two or more live sessions overlapping on a worktree or file.
type Conflict struct {
	Kind       string  `json:"kind"` // same_worktree|same_file|same_file_other_worktree
	Current    bool    `json:"current"`
	Family     bool    `json:"family"`
	Path       string  `json:"path"`
	SessionIDs []int64 `json:"session_ids"`
}

// Offset is a tail position in a log file.
type Offset struct {
	MachineID string    `json:"machine_id"`
	Inode     uint64    `json:"inode"`
	Size      int64     `json:"size_bytes"`
	Pos       int64     `json:"position_bytes"`
	Mtime     time.Time `json:"modified_at"`
}

// SourceStatus records a collection source's freshness.
type SourceStatus struct {
	MachineID   string    `json:"machine_id"`
	Name        string    `json:"name"`
	LastOKAt    time.Time `json:"last_ok_at"`
	LastError   string    `json:"last_error,omitempty"`
	LastErrorAt time.Time `json:"last_error_at"`
}

// Gap is a period the collector was not running.
type Gap struct {
	MachineID string    `json:"machine_id"`
	From      time.Time `json:"started_at"`
	To        time.Time `json:"ended_at"`
}

// Event is a stored hook event attributed to a session.
type Event struct {
	Metadata   *EventMetadata `json:"metadata,omitzero"`
	MachineID  string         `json:"machine_id"`
	Adapter    string         `json:"adapter"`
	ExternalID string         `json:"external_id"`
	TS         time.Time      `json:"occurred_at"`
	Kind       string         `json:"kind"`
	Summary    string         `json:"summary,omitempty"`
}
