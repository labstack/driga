package web

import (
	"context"
	"strings"
	"time"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/viewer"
)

type snapshotKey struct {
	scope string
	all   bool
}
type cachedSnapshot struct {
	snap viewer.Snapshot
	at   time.Time
}

// Cache only overview data. Fifteen-second expiry also refreshes ages without commits.
func (s *Server) cachedLoad(ctx context.Context, scope store.Scope, all bool) (viewer.Snapshot, error) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	version, err := s.source.DataVersion(ctx)
	if err != nil {
		return viewer.Snapshot{}, err
	}
	if version != s.cacheVersion {
		s.cache = map[snapshotKey]cachedSnapshot{}
		s.cacheVersion = version
	}
	key := snapshotKey{scope: strings.Join(scope.Machines, "\x00"), all: all}
	if scope.Machines != nil {
		key.scope = "selected:" + key.scope
	}
	now := time.Now()
	if c, ok := s.cache[key]; ok && now.Sub(c.at) < 15*time.Second {
		return c.snap, nil
	}
	snap, err := s.source.LoadScope(ctx, scope, all)
	if err == nil {
		if len(s.cache) >= 32 {
			s.cache = map[snapshotKey]cachedSnapshot{}
		}
		s.cache[key] = cachedSnapshot{snap, now}
	}
	return snap, err
}
func (s *Server) cachedCleanup(ctx context.Context) ([]cleanup.Candidate, error) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.cleanupAt.IsZero() || time.Since(s.cleanupAt) >= time.Minute {
		s.cleanupRows, s.cleanupErr = s.opts.CleanupList(ctx)
		s.cleanupAt = time.Now()
	}
	return s.cleanupRows, s.cleanupErr
}
