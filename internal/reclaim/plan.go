package reclaim

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

// Opportunity is an estimate and a reviewable next step, never authorization.
type Opportunity struct {
	RootPID        int32              `json:"root_pid,omitempty"`
	PPID           int32              `json:"ppid,omitempty"`
	StartedAt      time.Time          `json:"started_at,omitzero"`
	ObservedAt     time.Time          `json:"observed_at,omitzero"`
	Name           string             `json:"name,omitempty"`
	Cwd            string             `json:"cwd,omitempty"`
	Category       string             `json:"category,omitempty"`
	AgeMS          int64              `json:"age_ms,omitempty"`
	PIDs           []int32            `json:"pids,omitempty"`
	CPUKnown       *bool              `json:"cpu_known,omitempty"`
	IdentityKnown  *bool              `json:"identity_known,omitempty"`
	OwnershipKnown *bool              `json:"ownership_known,omitempty"`
	PeersKnown     *bool              `json:"peers_known,omitempty"`
	Simulator      *SimulatorReview   `json:"simulator,omitempty"`
	ID             string             `json:"id"`
	Kind           string             `json:"kind"`
	Status         string             `json:"status"`
	Reason         string             `json:"reason"`
	Memory         derive.MemoryUsage `json:"memory"`
	ResourcesKnown bool               `json:"resources_known"`
	CPU            float64            `json:"cpu_percent"`
	DiskBytes      int64              `json:"disk_bytes"`
	DiskKnown      bool               `json:"disk_size_known"`
	Action         string             `json:"action,omitempty"`
}

// Opportunities ranks actionable work first, then sampled CPU, memory and disk.
// It does not add unlike resources into a misleading combined savings score.
func Opportunities(disk []cleanup.Candidate, orphans []view.OrphanRow, sessions []model.Session, idx derive.ProcIndex) []Opportunity {
	rows := []Opportunity{}
	claimed := map[int32]bool{}
	for _, o := range orphans {
		r := Opportunity{ID: o.ID, Kind: "process", RootPID: o.RootPID, StartedAt: o.StartTime, Status: "review", Reason: strings.Join(o.Evidence, "; "), ResourcesKnown: o.ResourcesKnown, CPU: o.CPUPercent,
			Memory: derive.MemoryUsage{Processes: o.ProcCount, Memory: o.Memory, Measure: o.MemoryMeasure, RSS: o.RSSTotal}}
		if o.Young {
			r.Status = "protected"
			r.Reason = "too young to stop"
		} else if o.Verified && o.ResourcesKnown {
			r.Status = "ready"
			r.Action = "driga orphans kill " + o.ID
		}
		for _, e := range o.Evidence {
			if strings.HasPrefix(e, "ownership requires review:") {
				r.Status = "protected"
				r.Reason = e
				r.Action = ""
			}
		}
		if reason := derive.OrphanOwnershipReason(idx, sessions, o.RootPID); reason != "" {
			r.Status, r.Reason, r.Action = "protected", reason, ""
		}
		if !o.ResourcesKnown {
			r.Reason = "process identity or current tree resources unavailable; collect and review again"
			r.Action = ""
		}
		if r.Status == "review" && o.ResourcesKnown {
			// Snapshot candidates are not preverified. The existing command asks
			// for consent and proves idleness/ownership before signaling.
			r.Action = "driga orphans kill " + o.ID
		}
		rows = append(rows, r)
		claimed[o.RootPID] = true
		for _, pid := range o.PIDs {
			claimed[pid] = true
		}
	}
	// Attribute nested session trees once, to their outermost live session.
	ordered := append([]model.Session(nil), sessions...)
	sort.SliceStable(ordered, func(i, j int) bool { return len(idx.Ancestors(ordered[i].PID)) < len(idx.Ancestors(ordered[j].PID)) })
	for _, s := range ordered {
		if s.EndedAt != nil || s.Label == "dead" || s.Label == "ended" || s.PID <= 0 || claimed[s.PID] {
			continue
		}
		root, ok := idx.Get(s.PID)
		if !ok || s.ProcStart.IsZero() || !derive.SameStart(root.StartTime, s.ProcStart) {
			continue
		}
		members := []model.Proc{}
		cpu := 0.0
		for _, p := range append([]model.Proc{root}, idx.Descendants(root.PID)...) {
			if claimed[p.PID] {
				continue
			}
			claimed[p.PID] = true
			members = append(members, p)
			cpu += p.CPUPercent
		}
		rows = append(rows, Opportunity{ID: fmt.Sprintf("session:%d", s.ID), Kind: "session", RootPID: root.PID, StartedAt: root.StartTime, Status: "protected", Reason: "live session; open it to review work before stopping or restarting", Memory: derive.SumProcessMemory(members), ResourcesKnown: true, CPU: cpu, Action: fmt.Sprintf("driga agents jump %d", s.ID)})
	}
	for _, c := range disk {
		r := Opportunity{ID: c.ID, Kind: "disk", Status: c.Status, Reason: strings.Join(c.Evidence, "; "), DiskBytes: c.Size, DiskKnown: c.SizeKnown && !c.SizeCapped}
		if c.Status == "ready" {
			r.Action = "driga cleanup remove " + c.ID
		}
		rows = append(rows, r)
	}
	SortOpportunities(rows)
	return rows
}

func SortOpportunities(rows []Opportunity) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if (a.Status == "ready") != (b.Status == "ready") {
			return a.Status == "ready"
		}
		if a.CPU != b.CPU {
			return a.CPU > b.CPU
		}
		if a.Memory.Memory != b.Memory.Memory {
			return a.Memory.Memory > b.Memory.Memory
		}
		if a.DiskBytes != b.DiskBytes {
			return a.DiskBytes > b.DiskBytes
		}
		return a.ID < b.ID
	})
}
