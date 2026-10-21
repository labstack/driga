package collector

import (
	"slices"
	"sync"
	"time"

	"github.com/labstack/driga/internal/model"
)

// State retains sampling and process ownership evidence between passes. It is
// private to a collector run and is never written to disk.
type State struct {
	mu        sync.Mutex
	prev      map[int32]model.Proc
	prevAt    time.Time
	cwds      map[int32]cwdResult
	cwdsAt    time.Time
	cwdNext   int
	spawn     map[string]int64
	gitFullAt time.Time
	// procs is the process table this run last wrote, or nil before the first
	// successful scan, when the stored rows are the only source.
	procs []model.Proc
	logs  map[string]logBaseline
}

type cwdResult struct {
	start time.Time
	path  string
}

// NewState creates empty collector sampling state.
func NewState() *State {
	return &State{prev: make(map[int32]model.Proc), cwds: make(map[int32]cwdResult), spawn: make(map[string]int64)}
}

// copy returns an independent working state; a failed transaction must not
// advance CPU samples, cwd caches, or remembered process ownership. A pass
// replaces prev, spawn, procs and logs wholesale and never writes into them, so
// they are shared; only the cwd cache is updated in place and is copied.
func (s *State) copy() *State {
	n := &State{prev: s.prev, spawn: s.spawn, procs: s.procs, logs: s.logs, cwds: make(map[int32]cwdResult, len(s.cwds))}
	n.prevAt, n.cwdsAt, n.gitFullAt = s.prevAt, s.cwdsAt, s.gitFullAt
	n.cwdNext = s.cwdNext
	for k, v := range s.cwds {
		n.cwds[k] = v
	}
	return n
}

// storedProcs returns the last written process table, reading the store only
// until this run has written one. Callers own the returned slice.
func (s *State) storedProcs(read func() ([]model.Proc, error)) ([]model.Proc, error) {
	if s.procs != nil {
		return slices.Clone(s.procs), nil
	}
	return read()
}

func (s *State) applyCwds(procs []model.Proc) {
	for i, p := range procs {
		if c, ok := s.cwds[p.PID]; ok && c.start.Equal(p.StartTime) {
			procs[i].Cwd = c.path
		}
	}
}
