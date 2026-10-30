package web

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/viewer"
)

type snapshotKey struct {
	scope   string
	all     bool
	version int64
}
type cachedSnapshot struct {
	snap viewer.Snapshot
	at   time.Time
}
type snapshotLoad struct {
	done chan struct{}
	snap viewer.Snapshot
	err  error
}
type cleanupLoad struct {
	done chan struct{}
	rows []cleanup.Candidate
	err  error
}

// Cache only overview data. Fifteen-second expiry also refreshes ages without commits.
// Slow source reads are shared per key without holding the cache lock.
func (s *Server) cachedLoad(ctx context.Context, scope store.Scope, all bool) (viewer.Snapshot, error) {
	for {
		if err := ctx.Err(); err != nil {
			return viewer.Snapshot{}, err
		}
		version, err := s.source.DataVersion(ctx)
		if err != nil {
			return viewer.Snapshot{}, err
		}
		key := snapshotKey{scope: strings.Join(scope.Machines, "\x00"), all: all, version: version}
		if scope.Machines != nil {
			key.scope = "selected:" + key.scope
		}
		s.cacheMu.Lock()
		if c, ok := s.cache[key]; ok && time.Since(c.at) < 15*time.Second {
			s.cacheMu.Unlock()
			return c.snap, nil
		}
		if load := s.snapshotLoads[key]; load != nil {
			s.cacheMu.Unlock()
			select {
			case <-ctx.Done():
				return viewer.Snapshot{}, ctx.Err()
			case <-load.done:
				// A cancelled initiator must not poison another caller's request.
				if cancelledLoad(load.err) && ctx.Err() == nil {
					continue
				}
				return load.snap, load.err
			}
		}
		load := &snapshotLoad{done: make(chan struct{})}
		if s.snapshotLoads == nil {
			s.snapshotLoads = map[snapshotKey]*snapshotLoad{}
		}
		s.snapshotLoads[key] = load
		s.cacheMu.Unlock()

		load.snap, load.err = s.source.LoadScope(ctx, scope, all)
		s.cacheMu.Lock()
		delete(s.snapshotLoads, key)
		if load.err == nil {
			if s.cache == nil || len(s.cache) >= 32 {
				s.cache = map[snapshotKey]cachedSnapshot{}
			}
			s.cache[key] = cachedSnapshot{load.snap, time.Now()}
		}
		close(load.done)
		s.cacheMu.Unlock()
		return load.snap, load.err
	}
}

func (s *Server) cachedCleanup(ctx context.Context) ([]cleanup.Candidate, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s.cacheMu.Lock()
		if !s.cleanupAt.IsZero() && time.Since(s.cleanupAt) < time.Minute {
			rows := s.cleanupRows
			s.cacheMu.Unlock()
			return s.cleanupSizes.Snapshot(rows), nil
		}
		if load := s.cleanupLoad; load != nil {
			s.cacheMu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-load.done:
				if cancelledLoad(load.err) && ctx.Err() == nil {
					continue
				}
				if load.err != nil {
					return nil, load.err
				}
				return s.cleanupSizes.Snapshot(load.rows), nil
			}
		}
		load := &cleanupLoad{done: make(chan struct{})}
		s.cleanupLoad = load
		s.cacheMu.Unlock()

		load.rows, load.err = s.opts.CleanupList(ctx)
		s.cacheMu.Lock()
		s.cleanupLoad = nil
		if load.err == nil {
			s.cleanupRows, s.cleanupAt = load.rows, time.Now()
		}
		close(load.done)
		s.cacheMu.Unlock()
		if load.err != nil {
			return nil, load.err
		}
		return s.cleanupSizes.Snapshot(load.rows), nil
	}
}

func cancelledLoad(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
