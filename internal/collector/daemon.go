package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/scrub"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

// ErrAlreadyRunning indicates that another collector holds the writer lock.
var ErrAlreadyRunning = errors.New("collector already running")

// Intervals contains the independent source polling intervals.
type Intervals struct{ Procs, Git, Machine, GitFull, Cwds time.Duration }

func cadence(age time.Duration) Intervals {
	i := Intervals{Procs: time.Minute, Git: 5 * time.Minute, Machine: time.Minute, GitFull: 5 * time.Minute, Cwds: time.Minute}
	if age < 5*time.Second {
		i.Procs = 5 * time.Second
		i.Git = 30 * time.Second
		i.Machine = 10 * time.Second
	}
	return i
}

// Cadence returns polling intervals for the age of a viewer's heartbeat.
func Cadence(age time.Duration) Intervals { return cadence(age) }

// Lock takes the nonblocking collector writer lock and writes the owner's PID.
// Closing the returned file releases it; the lock file must not be removed.
func Lock(dataDir string) (*os.File, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dataDir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dataDir, "driga.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	failed := func(err error) (*os.File, error) { f.Close(); return nil, err }
	if err := f.Chmod(0600); err != nil {
		return failed(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			var buf [32]byte
			n, _ := f.ReadAt(buf[:], 0)
			pid, _ := strconv.Atoi(strings.TrimSpace(string(buf[:n])))
			return failed(fmt.Errorf("another driga collector is running (pid %d): %w", pid, ErrAlreadyRunning))
		}
		return failed(err)
	}
	if err := f.Truncate(0); err != nil {
		return failed(err)
	}
	if _, err := fmt.Fprintf(f, "%d\n", os.Getpid()); err != nil {
		return failed(err)
	}
	return f, nil
}

func recordGap(ctx context.Context, d Deps, st *store.Store) error {
	return st.Tx(ctx, func(tx *store.Tx) error {
		last, err := tx.Meta("last_pass_at")
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		from, err := time.Parse(time.RFC3339Nano, last)
		if err != nil {
			return fmt.Errorf("last_pass_at: %w", err)
		}
		now := d.now()
		if now.Sub(from) > 3*cadence(time.Hour).Procs {
			return tx.InsertGap(model.Gap{From: from, To: now})
		}
		return nil
	})
}

type schedule struct{ procs, machine, git, gitFull, cwds time.Time }

func due(last, now time.Time, interval time.Duration) bool {
	return last.IsZero() || !now.Before(last.Add(interval))
}

func (s *schedule) plan(now time.Time, i Intervals) Plan {
	return Plan{Procs: due(s.procs, now, i.Procs), Machine: due(s.machine, now, i.Machine), Git: due(s.git, now, i.Git) || due(s.gitFull, now, i.GitFull), GitFull: due(s.gitFull, now, i.GitFull), Cwds: due(s.cwds, now, i.Cwds), Sockets: due(s.procs, now, i.Procs)}
}

func (s *schedule) advance(p Plan, now time.Time) {
	if p.Procs {
		s.procs = now
	}
	if p.Machine {
		s.machine = now
	}
	if p.Git {
		s.git = now
	}
	if p.GitFull {
		s.gitFull = now
	}
	if p.Cwds {
		s.cwds = now
	}
}

func (s *schedule) wait(now time.Time, i Intervals) time.Duration {
	// Heartbeats must be noticed even while idle, before their five-second TTL.
	wait := time.Second
	for _, timer := range []struct {
		last     time.Time
		interval time.Duration
	}{{s.procs, i.Procs}, {s.machine, i.Machine}, {s.git, i.Git}, {s.gitFull, i.GitFull}, {s.cwds, i.Cwds}} {
		wait = min(wait, max(time.Duration(0), timer.last.Add(timer.interval).Sub(now)))
	}
	return wait
}

// Run collects until cancellation while holding the exclusive writer lock.
func Run(ctx context.Context, d Deps, dataDir string) error {
	if ctx.Err() != nil {
		return nil
	}
	lock, err := Lock(dataDir)
	if err != nil {
		return err
	}
	defer lock.Close()
	st, err := store.Open(filepath.Join(dataDir, "driga.db"), store.ReadWrite)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := recordGap(ctx, d, st); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	if d.State == nil {
		d.State = NewState()
	}
	var timers schedule
	lastPrune := time.Now()
	var spoolStamp time.Time
	for {
		if ctx.Err() != nil {
			return nil
		}
		age := time.Duration(1<<63 - 1)
		if info, err := os.Stat(filepath.Join(dataDir, "viewer.heartbeat")); err == nil {
			age = d.now().Sub(info.ModTime())
		}
		intervals := cadence(age)
		now := time.Now()
		p := timers.plan(now, intervals)
		// Append marks the directory even for existing files. Capture before
		// draining so concurrent appends are noticed on the next wake.
		var stamp time.Time
		if info, err := os.Stat(d.SpoolDir); err == nil {
			stamp = info.ModTime()
		}
		spoolChanged := !stamp.Equal(spoolStamp)
		spoolStamp = stamp
		if p.Procs || p.Machine || p.Git || p.Cwds {
			r, err := Pass(ctx, d, st, p)
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				return err
			}
			for name, message := range r.SourceErrors {
				slog.Warn("collector source failed", "source", name, "error", message)
			}
			timers.advance(p, now)
		} else if spoolChanged {
			if err := drainOnly(ctx, d, st); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
		if time.Since(lastPrune) >= time.Hour {
			if err := st.Tx(ctx, func(tx *store.Tx) error { return tx.Prune(d.now()) }); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			if err := spool.Prune(d.SpoolDir, d.now()); err != nil {
				slog.Warn("spool prune failed", "error", err)
			}
			lastPrune = time.Now()
		}
		timer := time.NewTimer(timers.wait(time.Now(), intervals))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func drainOnly(ctx context.Context, d Deps, st *store.Store) error {
	now := d.now()
	return st.Tx(ctx, func(tx *store.Tx) error {
		hooks, err := drainSpool(d, tx, now, func(name string, err error) error {
			if err != nil {
				return tx.SetSourceError(name, scrub.Scrub(err.Error()), now)
			}
			return tx.SetSourceOK(name, now)
		})
		if err != nil || len(hooks) == 0 {
			return err
		}
		ss, err := tx.Sessions(store.SessionFilter{IncludeEnded: true})
		if err != nil {
			return err
		}
		parents := make(map[int64]int64)
		byKey := make(map[string]int64)
		for _, s := range ss {
			if s.ParentID != nil {
				parents[s.ID] = *s.ParentID
			}
			byKey[sessionKey(s.Adapter, s.ExternalID)] = s.ID
		}
		base, err := tx.Meta("baseline_at")
		if err != nil {
			return err
		}
		baseline, err := time.Parse(time.RFC3339Nano, base)
		if err != nil {
			return err
		}
		for _, item := range derive.InboxItems(ss, parents, hooks, baseline) {
			if err := tx.UpsertInboxItem(item); err != nil {
				return err
			}
		}
		for _, h := range hooks {
			if id, ok := byKey[sessionKey(h.Adapter, h.SessionID)]; ok && h.Event == "Stop" && h.Message != "" {
				if err := tx.InsertHandoff(id, h.TS, h.Message); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
