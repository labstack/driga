package viewer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

// SourceFactory opens a daemon reader or a disposable collector and its cleanup.
type SourceFactory func(context.Context, bool) (ScopedSource, func(), error)

// SwitchingSource serializes reads with source replacement, so a database is
// closed only after its readers finish. Failed replacements leave it usable.
type SwitchingSource struct {
	mu                  sync.Mutex
	ctx                 context.Context
	running             func() bool
	open                SourceFactory
	source              ScopedSource
	closeSource         func()
	daemon              bool
	checked, refreshed  time.Time
	rawVersion, version int64
	Now                 func() time.Time
	active              bool
	activityKnown       bool
	closed              bool
}

func NewSwitchingSource(ctx context.Context, running func() bool, open SourceFactory) (*SwitchingSource, error) {
	s := &SwitchingSource{ctx: ctx, running: running, open: open, Now: time.Now}
	s.daemon = running()
	var err error
	s.source, s.closeSource, err = open(ctx, s.daemon)
	if err != nil && s.daemon {
		s.daemon = false
		s.source, s.closeSource, err = open(ctx, false)
	}
	if err != nil {
		return nil, err
	}
	s.checked = s.Now()
	s.version = 1
	return s, nil
}
func (s *SwitchingSource) recheck() error {
	if s.closed {
		return fmt.Errorf("viewer closed")
	}
	now := s.Now()
	if now.Sub(s.checked) < 5*time.Second {
		return nil
	}
	s.checked = now
	daemon := s.running()
	if daemon == s.daemon {
		return nil
	}
	next, closeNext, err := s.open(s.ctx, daemon)
	if err != nil {
		return nil
	} // retry on the next check without dropping the old reader
	if s.activityKnown {
		if a, ok := next.(interface{ SetActive(bool) }); ok {
			a.SetActive(s.active)
		}
	}
	oldClose := s.closeSource
	s.source, s.closeSource, s.daemon = next, closeNext, daemon
	s.version++
	s.refreshed = time.Time{}
	if oldClose != nil {
		oldClose()
	}
	return nil
}
func (s *SwitchingSource) Load(ctx context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recheck(); err != nil {
		return Snapshot{}, err
	}
	return s.source.Load(ctx)
}
func (s *SwitchingSource) LoadScope(ctx context.Context, scope store.Scope, all ...bool) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recheck(); err != nil {
		return Snapshot{}, err
	}
	return s.source.LoadScope(ctx, scope, all...)
}

// LoadFlow forwards to the current source; the web reads every page through
// this wrapper, so a missing method would leave the flow page always empty.
func (s *SwitchingSource) LoadFlow(ctx context.Context, scope store.Scope, id int64) (view.FlowView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recheck(); err != nil {
		return view.FlowView{}, err
	}
	f, ok := s.source.(FlowSource)
	if !ok {
		return view.FlowView{}, fmt.Errorf("flow is not available from this source")
	}
	return f.LoadFlow(ctx, scope, id)
}
func (s *SwitchingSource) DataVersion(ctx context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recheck(); err != nil {
		return 0, err
	}
	raw, err := s.source.DataVersion(ctx)
	if err != nil {
		return 0, err
	}
	now := s.Now()
	if raw != s.rawVersion || s.refreshed.IsZero() || now.Sub(s.refreshed) >= 15*time.Second {
		s.version++
		s.rawVersion = raw
		s.refreshed = now
	}
	return s.version, nil
}
func (s *SwitchingSource) LoadAgent(ctx context.Context, scope store.Scope, id int64) (view.AgentView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recheck(); err != nil {
		return view.AgentView{}, err
	}
	if d, ok := s.source.(DetailSource); ok {
		return d.LoadAgent(ctx, scope, id)
	}
	return view.AgentView{}, store.ErrNotFound
}
func (s *SwitchingSource) LoadProject(ctx context.Context, scope store.Scope, id int64, all bool) (view.WhereView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recheck(); err != nil {
		return view.WhereView{}, err
	}
	if d, ok := s.source.(DetailSource); ok {
		return d.LoadProject(ctx, scope, id, all)
	}
	return view.WhereView{}, store.ErrNotFound
}
func (s *SwitchingSource) SetActive(active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = active
	s.activityKnown = true
	if a, ok := s.source.(interface{ SetActive(bool) }); ok {
		a.SetActive(active)
	}
}
func (s *SwitchingSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		if s.closeSource != nil {
			s.closeSource()
		}
	}
	return nil
}

func (s *SwitchingSource) LoadFlowWindow(ctx context.Context, scope store.Scope, id int64, minutes int) (view.FlowView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recheck(); err != nil {
		return view.FlowView{}, err
	}
	if f, ok := s.source.(FlowWindowSource); ok {
		return f.LoadFlowWindow(ctx, scope, id, minutes)
	}
	if f, ok := s.source.(FlowSource); ok {
		return f.LoadFlow(ctx, scope, id)
	}
	return view.FlowView{}, fmt.Errorf("flow is not available from this source")
}
