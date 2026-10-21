package derive

import (
	"runtime"
	"sort"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
)

// ProcIndex indexes one process snapshot and its parent relationships.
type ProcIndex struct {
	procs    map[int32]model.Proc
	children map[int32][]int32
	pids     []int32
	windows  bool
}

// NewProcIndex builds an index of live local processes without retaining the
// caller's slice. Recorded and fleet samples must use NewProcIndexForOS.
func NewProcIndex(procs []model.Proc) ProcIndex {
	return NewProcIndexForOS(procs, runtime.GOOS)
}

// NewProcIndexForOS uses the process source's OS, independent of the viewer.
// An absent OS retains the legacy Unix topology used by older recordings.
func NewProcIndexForOS(procs []model.Proc, sourceOS string) ProcIndex {
	return newProcIndex(procs, sourceOS == "windows")
}

func newProcIndex(procs []model.Proc, windows bool) ProcIndex {
	idx := ProcIndex{procs: make(map[int32]model.Proc), children: make(map[int32][]int32), windows: windows}
	for _, p := range procs {
		idx.procs[p.PID] = p
	}
	for pid, p := range idx.procs {
		idx.pids = append(idx.pids, pid)
		if !windows || idx.windowsParent(p) {
			idx.children[p.PPID] = append(idx.children[p.PPID], pid)
		}
	}
	sort.Slice(idx.pids, func(i, j int) bool { return idx.pids[i] < idx.pids[j] })
	return idx
}

// Windows preserves the creator's PID after exit. A later process with that
// PID is unrelated. Unknown ages retain topology for presentation, but cannot
// establish orphan proof or authorize an action.
func (idx ProcIndex) windowsParent(child model.Proc) bool {
	parent, ok := idx.Get(child.PPID)
	return ok && (parent.StartTime.IsZero() || child.StartTime.IsZero() || !parent.StartTime.After(child.StartTime))
}

// Get returns the sampled process with pid.
func (idx ProcIndex) Get(pid int32) (model.Proc, bool) { p, ok := idx.procs[pid]; return p, ok }

// Ancestors returns parents nearest first, excluding pid and repeated pids.
func (idx ProcIndex) Ancestors(pid int32) []int32 {
	seen := map[int32]bool{pid: true}
	var result []int32
	for {
		p, ok := idx.Get(pid)
		if !ok || p.PPID <= 0 || seen[p.PPID] {
			return result
		}
		if idx.windows && !idx.windowsParent(p) {
			return result
		}
		pid = p.PPID
		seen[pid] = true
		result = append(result, pid)
	}
}

// Descendants returns all sampled descendants, excluding pid, in PID order.
// Broken or cyclic parent relationships cannot make a traversal repeat.
func (idx ProcIndex) Descendants(pid int32) []model.Proc {
	seen := map[int32]bool{pid: true}
	pending := []int32{pid}
	var result []model.Proc
	for len(pending) > 0 {
		parent := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, child := range idx.children[parent] {
			if seen[child] {
				continue
			}
			seen[child] = true
			result = append(result, idx.procs[child])
			pending = append(pending, child)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].PID < result[j].PID })
	return result
}

// LinkParents links sessions to their nearest live process ancestor. PID-zero
// sessions may instead be associated through a same-directory helper or host
// started within the two minutes preceding the session, nearest start first.
func LinkParents(sessions []model.Session, idx ProcIndex) map[int64]int64 {
	ordered := append([]model.Session(nil), sessions...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	byPID := familyLivePIDs(ordered, idx)
	byID := make(map[int64]model.Session)
	for _, s := range ordered {
		byID[s.ID] = s
	}
	links := make(map[int64]int64)
	sigs, _ := signatures.Load(nil)
	roles := make(map[int32]signatures.Role)
	for _, child := range ordered {
		if child.PID != 0 {
			for _, pid := range idx.Ancestors(child.PID) {
				if parent, ok := byPID[pid]; ok && parent != child.ID {
					familyLink(links, child.ID, parent)
					break
				}
			}
			continue
		}
		if child.Cwd == "" || child.StartedAt.IsZero() {
			continue
		}
		bestDepth := int(^uint(0) >> 1)
		fallbackDepth := bestDepth - 1
		var best int64
		var bestStart time.Time
		for _, pid := range idx.pids {
			p := idx.procs[pid]
			// Compare cwd before classifying: regex matching every process for
			// every PID-0 session dominated collector CPU.
			if p.Cwd != child.Cwd || p.StartTime.IsZero() || p.StartTime.After(child.StartedAt) || child.StartedAt.Sub(p.StartTime) > 2*time.Minute {
				continue
			}
			role, ok := roles[pid]
			if !ok {
				role = signatures.Role(p.Role)
				if role == "" && sigs != nil {
					sig, _ := sigs.Match(p.Cmdline, p.Exe)
					role = sig.Role
				}
				roles[pid] = role
			}
			if role != signatures.RoleHelper && role != signatures.RoleHost {
				continue
			}
			var candidate int64
			candidateDepth := fallbackDepth
			for depth, ancestor := range idx.Ancestors(pid) {
				parent, ok := byPID[ancestor]
				if !ok || parent == child.ID {
					continue
				}
				s := byID[parent]
				start := s.StartedAt
				if start.IsZero() {
					start = s.ProcStart
				}
				if start.IsZero() {
					proc, _ := idx.Get(s.PID)
					start = proc.StartTime
				}
				if p.StartTime.IsZero() || (!start.IsZero() && p.StartTime.Before(start)) || (s.EndedAt != nil && p.StartTime.After(*s.EndedAt)) {
					continue
				}
				candidate, candidateDepth = parent, depth
				break
			}
			// Detached brokers (PPID 1) carry no session in their ancestry. Fall
			// back to the latest live session whose lifetime includes this broker.
			if candidate == 0 {
				var fallbackStart time.Time
				for _, s := range ordered {
					if s.ID == child.ID || s.PID == 0 || s.Hosted || s.Cwd != child.Cwd || !familySessionLive(s) {
						continue
					}
					if _, ok := byPID[s.PID]; !ok {
						continue
					}
					start := s.StartedAt
					if start.IsZero() {
						start = s.ProcStart
					}
					if start.IsZero() || p.StartTime.IsZero() || p.StartTime.Before(start) {
						continue
					}
					if fallbackStart.IsZero() || start.After(fallbackStart) || start.Equal(fallbackStart) && s.ID < candidate {
						candidate, fallbackStart = s.ID, start
					}
				}
			}
			if candidate != 0 && (best == 0 || p.StartTime.After(bestStart) || p.StartTime.Equal(bestStart) && (candidateDepth < bestDepth || candidateDepth == bestDepth && candidate < best)) {
				best, bestDepth, bestStart = candidate, candidateDepth, p.StartTime
			}
		}
		if best != 0 {
			familyLink(links, child.ID, best)
		}
	}
	return links
}

func familyLink(links map[int64]int64, child, parent int64) {
	seen := map[int64]bool{child: true}
	for p := parent; p != 0; p = links[p] {
		if seen[p] {
			return
		}
		seen[p] = true
	}
	links[child] = parent
}

func familySessionLive(s model.Session) bool { return s.EndedAt == nil && s.Label != "dead" }

func familyLivePIDs(sessions []model.Session, idx ProcIndex) map[int32]int64 {
	result := make(map[int32]int64)
	for _, s := range sessions {
		p, ok := idx.Get(s.PID)
		if s.PID <= 0 || !familySessionLive(s) || !ok || (!s.ProcStart.IsZero() && !p.StartTime.IsZero() && !SameStart(p.StartTime, s.ProcStart)) {
			continue
		}
		if old, ok := result[s.PID]; !ok || s.ID < old {
			result[s.PID] = s.ID
		}
	}
	return result
}

// SpawnedBySession records the nearest live session ancestor for each process.
// Callers can retain these associations using PID plus process start time.
func SpawnedBySession(idx ProcIndex, sessions []model.Session) map[int32]int64 {
	live := familyLivePIDs(sessions, idx)
	result := make(map[int32]int64)
	for _, pid := range idx.pids {
		for _, ancestor := range idx.Ancestors(pid) {
			if id, ok := live[ancestor]; ok {
				result[pid] = id
				break
			}
		}
	}
	return result
}
