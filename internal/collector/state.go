package collector

import (
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
// advance CPU samples, cwd caches, or remembered process ownership.
func (s *State) copy() *State {
	n := NewState()
	n.prevAt, n.cwdsAt, n.gitFullAt = s.prevAt, s.cwdsAt, s.gitFullAt
	n.cwdNext = s.cwdNext
	for k, v := range s.prev {
		n.prev[k] = v
	}
	for k, v := range s.cwds {
		n.cwds[k] = v
	}
	for k, v := range s.spawn {
		n.spawn[k] = v
	}
	return n
}

func (s *State) applyCwds(procs []model.Proc) {
	for i, p := range procs {
		if c, ok := s.cwds[p.PID]; ok && c.start.Equal(p.StartTime) {
			procs[i].Cwd = c.path
		}
	}
}
