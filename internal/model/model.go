// Package model holds plain data types shared across driga's packages.
// It has no logic and no dependencies beyond the standard library.
package model

import "time"

// Machine is the host the collector runs on.
type Machine struct {
	ID        string    `json:"id"`
	Hostname  string    `json:"hostname"`
	OS        string    `json:"os"`
	Arch      string    `json:"arch"`
	Cores     int       `json:"cores"`
	MemTotal  uint64    `json:"mem_total"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

// Proc is one process in a snapshot.
type Proc struct {
	PID        int32     `json:"pid"`
	PPID       int32     `json:"ppid"`
	StartTime  time.Time `json:"start_time"`
	RSS        uint64    `json:"rss"`
	CPUSeconds float64   `json:"cpu_seconds"`
	CPUPercent float64   `json:"cpu_percent"`
	Cmdline    string    `json:"cmdline"`
	Exe        string    `json:"exe"`
	Cwd        string    `json:"cwd"`
	Role       string    `json:"role,omitempty"`
	Agent      string    `json:"agent,omitempty"`
	SessionID  int64     `json:"session_id,omitempty"`
}

// MachineSample is one machine-health reading.
type MachineSample struct {
	TS            time.Time `json:"ts"`
	MemUsed       uint64    `json:"mem_used"`
	MemCompressed uint64    `json:"mem_compressed"`
	MemTotal      uint64    `json:"mem_total"`
	SwapUsed      uint64    `json:"swap_used"`
	SwapTotal     uint64    `json:"swap_total"`
	DiskFree      uint64    `json:"disk_free"`
	PressureFree  float64   `json:"pressure_free"`
	Load1         float64   `json:"load1"`
	Load5         float64   `json:"load5"`
	Load15        float64   `json:"load15"`
	Cores         int       `json:"cores"`
}

// Repo is a git repository (the main worktree's path).
type Repo struct {
	ID        int64  `json:"id"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	RemoteURL string `json:"remote_url,omitempty"`
}

// Worktree is one checkout of a repo.
type Worktree struct {
	ID                int64     `json:"id"`
	RepoID            int64     `json:"repo_id"`
	Path              string    `json:"path"`
	Branch            string    `json:"branch"`
	Head              string    `json:"head"`
	Ahead             int       `json:"ahead"`
	Behind            int       `json:"behind"`
	Dirty             int       `json:"dirty"`
	Untracked         int       `json:"untracked"`
	OldestDirtyMtime  time.Time `json:"oldest_dirty_mtime"`
	LastCommitAt      time.Time `json:"last_commit_at"`
	LastCommitSubject string    `json:"last_commit_subject"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// SessionObs is what an adapter observed about one agent session.
type SessionObs struct {
	Adapter        string            `json:"adapter"`
	ExternalID     string            `json:"external_id"`
	Origin         string            `json:"origin"`
	Version        string            `json:"version,omitempty"`
	PID            int32             `json:"pid"`
	ProcStart      time.Time         `json:"proc_start"`
	Cwd            string            `json:"cwd"`
	Branch         string            `json:"branch,omitempty"`
	Title          string            `json:"title,omitempty"`
	LastPrompt     string            `json:"last_prompt,omitempty"`
	RegistryStatus string            `json:"registry_status,omitempty"`
	StartedAt      time.Time         `json:"started_at"`
	LastActivityAt time.Time         `json:"last_activity_at"`
	LastPromptAt   time.Time         `json:"last_prompt_at"`
	Hosted         bool              `json:"hosted"`
	Completions    []CompletionEvent `json:"completions,omitempty"`
	FilesTouched   []FileTouch       `json:"files_touched,omitempty"`
	TurnOpen       bool              `json:"turn_open"`
	SourcePath     string            `json:"source_path,omitempty"`
}

// CompletionEvent is one finished turn.
type CompletionEvent struct {
	ID   string    `json:"id"`
	TS   time.Time `json:"ts"`
	Text string    `json:"text,omitempty"`
}

// FileTouch is one file edited by a session.
type FileTouch struct {
	Path string    `json:"path"`
	TS   time.Time `json:"ts"`
}

// HookEvent is one line in the spool. Event is one of
// Stop|Notification|SessionStart|SessionEnd|notify|ack; for ack, Message is
// the inbox item id.
type HookEvent struct {
	TS        time.Time `json:"ts"`
	Adapter   string    `json:"adapter"`
	SessionID string    `json:"session_id"`
	Cwd       string    `json:"cwd,omitempty"`
	Event     string    `json:"event"`
	Message   string    `json:"message,omitempty"`
}

// Session is a stored session with derived fields.
type Session struct {
	ID       int64  `json:"id"`
	ParentID *int64 `json:"parent_id,omitempty"`
	SessionObs
	RepoID     *int64     `json:"repo_id,omitempty"`
	WorktreeID *int64     `json:"worktree_id,omitempty"`
	Label      string     `json:"label"`
	Heuristic  bool       `json:"heuristic"`
	Evidence   string     `json:"evidence"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
}

// InboxItem is one event needing the user's attention.
type InboxItem struct {
	ID        string     `json:"id"`
	SessionID int64      `json:"session_id"`
	Kind      string     `json:"kind"` // finished|waiting
	TS        time.Time  `json:"ts"`
	Summary   string     `json:"summary"`
	AckedAt   *time.Time `json:"acked_at,omitempty"`
}

// Orphan is a process tree left behind by a dead agent session.
type Orphan struct {
	ID        string    `json:"id"` // root_pid:start_unix
	RootPID   int32     `json:"root_pid"`
	StartTime time.Time `json:"start_time"`
	Signature string    `json:"signature"`
	ProcCount int       `json:"proc_count"`
	RSSTotal  uint64    `json:"rss_total"`
	Cwd       string    `json:"cwd"`
	Young     bool      `json:"young"`
	Verified  bool      `json:"verified"` // idle with no external peers on consecutive passes
	Evidence  []string  `json:"evidence"`
	FirstSeen time.Time `json:"first_seen"`
	PIDs      []int32   `json:"pids"`
}

// Conflict is two or more live sessions overlapping on a worktree or file.
type Conflict struct {
	Kind       string  `json:"kind"` // same-worktree|same-file|same-file-other-worktree
	Current    bool    `json:"current"`
	Family     bool    `json:"family"`
	Path       string  `json:"path"`
	SessionIDs []int64 `json:"session_ids"`
}

// Offset is a tail position in a log file.
type Offset struct {
	Inode uint64    `json:"inode"`
	Size  int64     `json:"size"`
	Pos   int64     `json:"pos"`
	Mtime time.Time `json:"mtime"`
}

// SourceStatus records a collection source's freshness.
type SourceStatus struct {
	Name        string    `json:"name"`
	LastOKAt    time.Time `json:"last_ok_at"`
	LastError   string    `json:"last_error,omitempty"`
	LastErrorAt time.Time `json:"last_error_at"`
}

// Gap is a period the collector was not running.
type Gap struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// Event is a stored hook event attributed to a session.
type Event struct {
	Adapter    string    `json:"adapter"`
	ExternalID string    `json:"external_id"`
	TS         time.Time `json:"ts"`
	Kind       string    `json:"kind"`
	Summary    string    `json:"summary,omitempty"`
}
