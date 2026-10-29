// Package command prepares local command requests for broker authorization.
package command

import (
	"crypto/sha256"
	"encoding/json"
)

type Request struct {
	Argv           []string `json:"argv,omitempty"`
	Script         string   `json:"script,omitempty"`
	Cwd            string   `json:"cwd"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	Network        bool     `json:"network"`
}

type Scope struct {
	Project, Scratch string
	Denied           []string
}
type Class string

const (
	Read        Class = "Read"
	Process     Class = "Process"
	Other       Class = "Other"
	Destructive Class = "Destructive"
	Never       Class = "Never"
)

// Parsed contains copies of the requested form and literal AST commands. Complex
// scripts retain their exact source; they never inherit literal-read authority.
type Parsed struct {
	Request  Request
	Commands [][]string
	Pipeline bool
	Complex  bool
}

type Executable struct {
	Path   string
	Argv   []string
	SHA256 [32]byte
}
type Policy struct {
	Class    Class
	Reason   string
	Commands []Executable
	// RequiresCard excludes unconfined reads from Full auto and similarity grants.
	RequiresCard bool
}

// Prepared is a value snapshot. Callers must deep-copy slices at trust boundaries;
// Digest covers every disclosed execution field, including later target snapshots.
type Prepared struct {
	Parsed            Parsed
	Scope             Scope
	Policy            Policy
	Sandbox           Capability
	BackendExecutable Executable
	Profile           string
	WritableRoots     []string
	ProcessSnapshot   *ProcessSnapshot
	seal              [32]byte
}

func Digest(p Prepared) [32]byte { b, _ := json.Marshal(p); return sha256.Sum256(b) }
