package derive

import (
	"github.com/labstack/driga/internal/pathx"
	"regexp"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
)

type Bucket string

const (
	BucketSession Bucket = "session"
	BucketAgent   Bucket = "agent"
	BucketLikely  Bucket = "likely"
	BucketOther   Bucket = "other"
)

type ProcAttribution struct {
	SessionID int64
	Project   string
	Proof     string
}
type ProcPlacement struct {
	PID         int32
	Bucket      Bucket
	Attribution *ProcAttribution
	Bundle      string
	SharedHost  int32
	Memory      uint64
	Measure     string
}

// MCPNameToken recognizes the MCP token in a parsed package/command name,
// rather than incidental arguments or directory names.
func MCPNameToken(name string) bool { return mcpToken.MatchString(name) }

var mcpToken = regexp.MustCompile(`(?i)(^|[^a-z0-9])mcp($|[^a-z0-9])`)

// PlaceProcesses places every non-zombie process exactly once using only the
// existing sample. Stored orphan ancestry is not live-session evidence.
func PlaceProcesses(procs []model.Proc, sessions []model.Session, sigs *signatures.Set) map[int32]ProcPlacement {
	if sigs == nil {
		sigs = signatures.Builtin()
	}
	byPID := make(map[int32]model.Proc, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}
	live := map[int32]model.Session{}
	for _, s := range sessions {
		p, ok := byPID[s.PID]
		if !ok || s.ID == 0 || s.EndedAt != nil || s.Label == "dead" || s.Hosted || s.ProcStart.IsZero() || !SameStart(p.StartTime, s.ProcStart) {
			continue
		}
		live[s.PID] = s
	}
	proven := make(map[int32]bool, len(procs))
	mcp := make(map[int32]bool, len(procs))
	for _, p := range procs {
		sig, _ := sigs.Match(p.Cmdline, p.Exe)
		agentRole := func(role signatures.Role) bool {
			switch role {
			case signatures.RoleInteractive, signatures.RoleHeadless, signatures.RoleHost, signatures.RoleService, signatures.RoleHelper:
				return true
			}
			return false
		}
		proven[p.PID] = (sig.Agent != "" && agentRole(sig.Role)) || (p.Agent != "" && agentRole(signatures.Role(p.Role)))

		mcp[p.PID] = sig.Name == "generic-mcp-launcher" || MCPNameToken(describeMCP(p, sigs).name)
	}
	result := make(map[int32]ProcPlacement, len(procs))
	for _, p := range procs {
		if p.Zombie {
			continue
		}
		placement := ProcPlacement{PID: p.PID, Bucket: BucketOther}
		placement.Memory, placement.Measure = ProcessMemory(p)
		agent, likely, appBoundary := false, false, false
		var shared int32
		bundle := ""
		for cur := range processChain(byPID, p.PID) {
			if bundle == "" {
				bundle = AppBundle(cur.Exe)
			}
			if s, ok := live[cur.PID]; ok && placement.Attribution == nil {
				placement.Attribution = &ProcAttribution{SessionID: s.ID, Project: pathx.RecordedBase(s.Cwd), Proof: "parent_chain"}
			}

			if proven[cur.PID] {
				agent = true
			}
			// MCP evidence must be below the nearest non-agent app, never
			// inherited through an editor's own windows or extension workers.
			if !appBoundary {
				// MCP evidence wins over the bundle: editors may run MCP servers
				// with their own bundled runtime.
				if mcp[cur.PID] && shared == 0 {
					shared = cur.PPID
					likely = true
				} else if AppBundle(cur.Exe) != "" && !proven[cur.PID] {
					appBoundary = true
				}
			}
		}
		// An agent process in the chain is stronger evidence than a shared cwd:
		// that agent owns its own tree.
		if placement.Attribution == nil && !agent && p.Cwd != "" && !p.StartTime.IsZero() {
			var chosen *model.Session
			for _, s := range live {
				if s.StartedAt.IsZero() || !p.StartTime.After(s.StartedAt) || !bucketWithin(p.Cwd, s.Cwd) {
					continue
				}
				// Multiple sessions in the same best-matching project cannot prove which
				// session started the process. Prefer a uniquely more specific project.
				if chosen == nil || len(s.Cwd) > len(chosen.Cwd) {
					copy := s
					chosen = &copy
				} else if len(s.Cwd) == len(chosen.Cwd) {
					chosen.ID = 0
				}
			}
			if chosen != nil && chosen.ID != 0 {
				placement.Attribution = &ProcAttribution{SessionID: chosen.ID, Project: pathx.RecordedBase(chosen.Cwd), Proof: "cwd_started_after"}
			}
		}

		placement.Bundle = bundle
		switch {
		case placement.Attribution != nil:
			placement.Bucket = BucketSession
		case agent:
			placement.Bucket = BucketAgent
		case likely:
			placement.Bucket = BucketLikely
			placement.SharedHost = shared
		}
		result[p.PID] = placement
	}
	return result
}

func bucketWithin(p, root string) bool { return pathx.RecordedWithin(p, root) }
