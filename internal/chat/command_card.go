package chat

import (
	"encoding/json"
	"fmt"
	"github.com/labstack/driga/internal/command"
	"github.com/labstack/driga/internal/model"
	"path/filepath"
	"strings"
	"time"
)

// CommandCard is presentation only. Execution authority stays broker-private.
type CommandCard struct {
	Requested   command.Request `json:"requested"`
	Effective   [][]string      `json:"effective"`
	Cwd         string          `json:"cwd"`
	Class       command.Class   `json:"class"`
	Summary     string          `json:"summary"`
	Boundary    string          `json:"boundary"`
	Error       string          `json:"error,omitempty"`
	Targets     []ProcessRow    `json:"targets"`
	Count       int             `json:"count"`
	MemoryBytes uint64          `json:"memory_bytes"`
	Force       bool            `json:"force"`
	Stopped     []ProcessRow    `json:"stopped,omitempty"`
	Vanished    []ProcessRow    `json:"vanished,omitempty"`
	Skipped     []ProcessRow    `json:"skipped,omitempty"`
	Survivors   []ProcessRow    `json:"survivors,omitempty"`
}
type ProcessRow struct {
	PID         int32     `json:"pid"`
	Owner       string    `json:"owner"`
	StartedAt   time.Time `json:"started_at"`
	Executable  string    `json:"executable"`
	Argv        []string  `json:"argv"`
	CommandLine string    `json:"command_line"`
	MemoryBytes uint64    `json:"memory_bytes"`
	Reason      string    `json:"reason,omitempty"`
}

func processRow(p model.Proc) ProcessRow {
	owner := fmt.Sprintf("UID %d", p.OwnerUID)
	if p.OwnerSID != "" {
		owner = "SID " + p.OwnerSID
	}
	if !p.OwnerKnown {
		owner = "unknown owner"
	}
	memory := p.Footprint
	if memory == 0 {
		memory = p.RSS
	}
	return ProcessRow{PID: p.PID, Owner: owner, StartedAt: p.StartTime, Executable: p.Exe, Argv: append([]string(nil), p.Argv...), CommandLine: p.Cmdline, MemoryBytes: memory}
}
func PresentCommand(p command.Prepared) CommandCard {
	c := CommandCard{Requested: p.Parsed.Request, Cwd: p.Parsed.Request.Cwd, Class: p.Policy.Class, Targets: []ProcessRow{}, Effective: [][]string{}}
	c.Requested.Argv = append([]string(nil), c.Requested.Argv...)
	if p.Policy.Class != command.Process {
		if argv, err := command.EffectiveArgv(p); err == nil {
			c.Effective = append(c.Effective, argv)
		} else {
			c.Error = err.Error()
		}
	}

	roots := p.Scope.Project
	if roots == "" {
		roots = p.Scope.Scratch
	} else {
		roots += " and " + p.Scope.Scratch
	}
	if len(p.WritableRoots) > 0 {
		roots = strings.Join(p.WritableRoots, " and ")
	}
	network := "no network"
	if p.Parsed.Request.Network {
		network = "network allowed"
	}
	c.Boundary = "sandboxed: writes only to " + roots + ", " + network
	c.Summary = "Run command"
	if !p.Sandbox.Available {
		if c.Class != command.Read && c.Class != command.Process {
			c.Error = p.Sandbox.Reason
		}
		c.Boundary += " · sandbox unavailable"
		if c.Class == command.Read {
			c.Boundary = "Validated Read only · sandbox unavailable · no writes · no network"
		}
	}
	if p.ProcessSnapshot != nil {
		c.Force = p.ProcessSnapshot.Force
		for _, t := range p.ProcessSnapshot.Targets {
			v := t.Proc
			v.OwnerUID = t.OwnerUID
			v.OwnerSID = t.OwnerSID
			v.OwnerKnown = t.OwnerKnown
			r := processRow(v)
			c.Targets = append(c.Targets, r)
			c.MemoryBytes += r.MemoryBytes
		}
		c.Count = len(c.Targets)
		c.Summary = fmt.Sprintf("Stop %d processes owned by you", c.Count)
		if a := p.Parsed.Request.Argv; len(a) > 1 && (filepath.Base(a[0]) == "pkill" || filepath.Base(a[0]) == "killall") {
			c.Summary += " matching `" + a[len(a)-1] + "`"
		}
		if c.MemoryBytes > 0 {
			if c.MemoryBytes >= 1e9 {
				c.Summary += fmt.Sprintf(" (about %.0f GB)", float64(c.MemoryBytes)/1e9)
			} else {
				c.Summary += fmt.Sprintf(" (about %.0f MB)", float64(c.MemoryBytes)/1e6)
			}
		}
		if c.Force {
			c.Summary = fmt.Sprintf("Force stop %d surviving processes owned by you", c.Count)
		}
		c.Boundary = "Host process snapshot only · no new pattern match · " + map[bool]string{false: "SIGTERM", true: "SIGKILL · second approval"}[c.Force]
	}
	return c
}
func copyCommandCard(c CommandCard) CommandCard {
	// Keep independent slices for state and event consumers, including result rows.
	raw, _ := json.Marshal(c)
	var out CommandCard
	_ = json.Unmarshal(raw, &out)
	return out
}
