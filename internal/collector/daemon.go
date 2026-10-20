package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/fleet"
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

// Running reports whether the PID recorded by the lock holder is alive. It
// only reads the lock file: probing with flock could make a collector that is
// starting at the same moment fail. A reused PID can at worst make a reader
// trust a recent database for its freshness window.
func Running(dataDir string) bool {
	b, err := os.ReadFile(filepath.Join(dataDir, "driga.lock"))
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return false
	}
	err = syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

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
			gap := model.Gap{MachineID: tx.MachineID(), From: from, To: now}
			if err := tx.InsertGap(gap); err != nil {
				return err
			}
			return appendFeed(tx, feed.Gap, gap, feed.None, now)
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

func (s *schedule) wait(_ time.Time, _ Intervals) time.Duration {
	// Coalesce source deadlines onto the heartbeat/spool check. An overdue
	// source must not create zero-delay catch-up wakes after a slow import.
	return time.Second
}

// Run collects until cancellation while holding the exclusive writer lock.
func Run(ctx context.Context, d Deps, dataDir string) error {
	if ctx.Err() != nil {
		return nil
	}
	clock := d.clock
	if clock == nil {
		clock = realDaemonClock{}
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
	if err := MigrateSpool(ctx, d, st); err != nil {
		return err
	}
	// Commit export-policy changes before any listener can serve the retained
	// feed. The first pass may spend a long time collecting recent histories.
	if err := st.Tx(ctx, func(tx *store.Tx) error {
		_, err := configurePassFeed(d, tx)
		return err
	}); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	intakeRequests := make(chan intakeRequest, 16)
	stopIntake, err := startIntake(ctx, d, st, intakeRequests)
	if err != nil {
		return err
	}
	if stopIntake != nil {
		defer stopIntake()
	}
	fleetErrors := make(chan error, 1)
	server, stopFleet := startFleetServer(ctx, d, dataDir, fleetErrors)
	if stopFleet != nil {
		defer stopFleet()
	}
	if err := recordGap(ctx, d, st); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	if d.State == nil {
		d.State = NewState()
	}
	pullers := newFleetPullers(dataDir)
	pullers.server = server
	defer pullers.close()
	var timers schedule
	lastPrune := clock.Now()
	var spoolStamp time.Time
	var lastPoll time.Time
	age := time.Duration(1<<63 - 1)
	for {
		if ctx.Err() != nil {
			return nil
		}
		now := clock.Now()
		poll := due(lastPoll, now, time.Second)
		if poll {
			lastPoll = now
		}
		if poll && server != nil {
			// Reuse the collector wake instead of a timer per presence stream.
			_ = server.RefreshPresence()
			server.ProcessControl(ctx, st)
		}
		select {
		case serveErr := <-fleetErrors:
			if err := st.Tx(ctx, func(tx *store.Tx) error {
				now := d.now()
				if err := tx.SetSourceError("fleet", scrub.Scrub(serveErr.Error()), now); err != nil {
					return err
				}
				sources, err := tx.Sources()
				if err != nil {
					return err
				}
				for _, source := range sources {
					if source.Name == "fleet" {
						return appendFeed(tx, feed.Source, source, feed.None, now)
					}
				}
				return nil
			}); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		default:
		}
		if poll {
			age = time.Duration(1<<63 - 1)
			if info, err := os.Stat(filepath.Join(dataDir, "viewer.heartbeat")); err == nil {
				age = d.now().Sub(info.ModTime())
			}
		}
		var fleetErr error
		if poll {
			fleetErr = pullers.wake(ctx, d, st, server != nil, age < 5*time.Second)
		} else {
			fleetErr = pullers.applyReady(ctx, d, st)
		}
		if fleetErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fleetErr
		}
		intervals := cadence(age)
		p := Plan{}
		if poll {
			p = timers.plan(now, intervals)
		}
		// Append marks the directory even for existing files. Capture before
		// draining so concurrent appends are noticed on the next wake.
		spoolChanged := false
		if poll {
			var stamp time.Time
			if info, err := os.Stat(d.SpoolDir); err == nil {
				stamp = info.ModTime()
			}
			spoolChanged = !stamp.Equal(spoolStamp)
			spoolStamp = stamp
		}
		if p.Procs || p.Machine || p.Git || p.Cwds {
			r, err := Pass(ctx, d, st, p)
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				return err
			}
			for name, message := range r.SourceErrors {
				slog.Warn("collector source failed", "event", "collector_source_failed", "source", name, "err", message)
			}
			// Long source reads must leave a full cadence before collecting
			// again, rather than keeping the collector continuously busy.
			timers.advance(p, clock.Now())
			if server != nil {
				server.Notify()
			}
		} else if spoolChanged {
			if err := drainOnly(ctx, d, st); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			if server != nil {
				server.Notify()
			}
		}
		if err := alertInbox(ctx, d, st); err != nil {
			return err
		}
		if clock.Now().Sub(lastPrune) >= time.Hour {
			if err := st.Tx(ctx, func(tx *store.Tx) error { return tx.Prune(d.now()) }); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			if err := spool.Prune(d.SpoolDir, d.now()); err != nil {
				slog.Warn("spool prune failed", "event", "spool_prune_failed", "err", scrub.Scrub(err.Error()))
			}
			lastPrune = clock.Now()
			if server != nil {
				server.Notify()
			}
		}
		timer := clock.NewTimer(timers.wait(clock.Now(), intervals))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.Chan():
		case req := <-intakeRequests:
			timer.Stop()
			if req.stats != nil {
				if err := recordIntakeStats(ctx, st, req.stats()); err != nil {
					if req.reply != nil {
						req.reply <- intakeResult{err: err}
					}
					if ctx.Err() != nil {
						return nil
					}
					return err
				}
				if req.reply == nil {
					if server != nil {
						server.Notify()
					}
					continue
				}
			}
			if req.failure != nil {
				if err := recordIntakeError(ctx, d, st, req.failure); err != nil {
					if ctx.Err() != nil {
						return nil
					}
					return err
				}
				if server != nil {
					server.Notify()
				}
				continue
			}
			_, err := applyIntake(ctx, d, st, req.batch)
			req.reply <- intakeResult{err: err}
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			if server != nil {
				server.Notify()
			}
		case b := <-pullers.batches:
			timer.Stop()
			pullers.ready = append(pullers.ready, b)
		}
	}
}

// startFleetServer keeps serving failures isolated from local collection.
func startFleetServer(ctx context.Context, d Deps, dataDir string, failures chan<- error) (*fleet.Server, func()) {
	if !d.Cfg.Fleet.Serve {
		return nil, nil
	}
	fail := func(err error) (*fleet.Server, func()) {
		slog.Warn("fleet serving unavailable", "event", "fleet_serve_failed", "err", scrub.Scrub(err.Error()))
		select {
		case failures <- err:
		default:
		}
		return nil, nil
	}
	addr, err := fleet.CheckListen(d.Cfg.Fleet.Listen, d.Cfg.Fleet.AllowAnyAddress)
	if err != nil {
		return fail(err)
	}
	id, err := fleet.LoadOrCreateIdentity(filepath.Join(dataDir, "fleet"))
	if err != nil {
		return fail(err)
	}
	ro, err := store.Open(filepath.Join(dataDir, "driga.db"), store.ReadOnly)
	if err != nil {
		return fail(err)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		ro.Close()
		return fail(err)
	}
	server := fleet.NewServer(ro, id, dataDir, d.Cfg)
	server.SpoolDir = d.SpoolDir
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer ro.Close()
		if err := server.Serve(runCtx, ln); err != nil {
			_, _ = fail(err)
		}
	}()
	return server, func() { cancel(); <-done }
}

func drainOnly(ctx context.Context, d Deps, st *store.Store) error {
	now := d.now()
	share := feed.EffectiveShare(feed.ShareText(d.Cfg.Fleet.ShareText), d.Cfg.CaptureText)
	err := st.Tx(ctx, func(tx *store.Tx) error {
		before, err := passFeedRows(tx, now, share)
		if err != nil {
			return err
		}
		publish := func() error {
			after, err := passFeedRows(tx, now, share)
			if err != nil {
				return err
			}
			return appendPassChanges(tx, before, after, now)
		}
		hooks, err := drainSpool(d, tx, now, func(name string, err error) error {
			if err != nil {
				return tx.SetSourceError(name, scrub.Scrub(err.Error()), now)
			}
			return tx.SetSourceOK(name, now)
		})
		if err != nil {
			return err
		}
		if len(hooks) == 0 {
			return publish()
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
		ss = hookSessions(ss, hooks)
		for i := range ss {
			if ss[i].ID == 0 {
				id, e := tx.UpsertSession(ss[i])
				if e != nil {
					return e
				}
				ss[i].ID = id
			}
		}
		if err := syncInbox(tx, d, ss, parents, hooks, baseline); err != nil {
			return err
		}
		if err := tx.CloseAttentionGrowth(now); err != nil {
			return err
		}
		if err := resolveInboxJumps(ctx, tx, d, ss, nil); err != nil {
			return err
		}
		for _, h := range hooks {
			if id, ok := byKey[sessionKey(h.Adapter, h.SessionID)]; ok && h.Event == "Stop" && h.Message != "" {
				if err := tx.InsertHandoff(id, h.TS, h.Message); err != nil {
					return err
				}
				if err := appendFeed(tx, feed.Handoff, feed.HandoffRow{MachineID: tx.MachineID(), Session: feed.SessionKey{Adapter: h.Adapter, ExternalID: h.SessionID}, TS: h.TS, Text: h.Message}, share, now); err != nil {
					return err
				}
			}
		}
		return publish()
	})
	if err != nil {
		return err
	}
	return alertInbox(ctx, d, st)
}
