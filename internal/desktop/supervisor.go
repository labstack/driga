// Package desktop runs Driga as a desktop app: it supervises the collector as
// a child process and relays what the collector reports.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/labstack/driga/internal/attention"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/collector"
)

// DefaultBackoff is the wait before each restart of a collector that exited.
// The last step repeats.
var DefaultBackoff = []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second}

const (
	// healthyRun resets the backoff: a child that ran this long did not crash
	// on startup.
	healthyRun = 5 * time.Minute
	// defaultPoll is how often the lock is checked while another collector holds it.
	defaultPoll = 5 * time.Second
	// orphanWait bounds how long a terminated orphan may take to free the lock.
	orphanWait = 10 * time.Second
	orphanPoll = 100 * time.Millisecond
	// readyTimeout bounds one check that a new child answers on the chat socket.
	readyTimeout = 10 * time.Second
)

// readyBackoff is the wait before each further check of a child that does
// not answer yet. The last step repeats.
var readyBackoff = []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second}

// Child is a running collector process.
type Child interface {
	// Wait blocks until the child exits. It is called once.
	Wait() error
	// Stop asks the child to exit and returns once it has.
	Stop() error
	// Notifications yields the child's notification stream; it ends when the
	// child exits. A nil reader means the child sends none.
	Notifications() io.Reader
}

// Status describes the collector as the supervisor sees it.
type Status struct {
	// Collecting is true while the app's own child runs and answers.
	Collecting bool
	// Owner is the other process holding the collector lock, if any.
	Owner collector.Owner
	Err   error
	// RestartPending is the app's: an upgrade waits for it to restart, and
	// no collector runs until then.
	RestartPending bool
}

// Supervisor keeps one collector running for the app. When the lock is free it
// starts a child and restarts it with backoff; when another collector holds
// the lock it reports that owner and takes over once it exits.
type Supervisor struct {
	Start     func(ctx context.Context) (Child, error)
	ReadOwner func() (collector.Owner, bool, error)
	// Ready returns once a new child serves chat, or with why it does not;
	// WaitChat is the production check. Nil counts a child ready at once.
	Ready  func(ctx context.Context) error
	Notify func(attention.Notification)
	Status func(Status)
	// Backoff defaults to DefaultBackoff; Poll defaults to five seconds.
	Backoff []time.Duration
	Poll    time.Duration
	// Executable is the app's own binary. An app collector of the same binary
	// still holding the lock is an orphan of an earlier run of this app: its
	// notification pipe is gone, so it is terminated and replaced.
	Executable string
	// Terminate defaults to TerminateProcess.
	Terminate func(pid int) error

	clock clock
	last  *Status
}

type clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Run supervises until ctx is cancelled, then stops the child and waits for it.
func (s *Supervisor) Run(ctx context.Context) error {
	if s.clock == nil {
		s.clock = realClock{}
	}
	backoff := s.Backoff
	if len(backoff) == 0 {
		backoff = DefaultBackoff
	}
	poll := s.Poll
	if poll <= 0 {
		poll = defaultPoll
	}
	step := 0
	for ctx.Err() == nil {
		owner, held, err := s.ReadOwner()
		if err != nil {
			s.report(Status{Err: fmt.Errorf("read collector lock: %w", err)})
			s.sleep(ctx, poll)
			continue
		}
		if held {
			if owner.Kind != "app" || owner.Executable != s.Executable {
				// Includes the "unknown" kind: a holder that has not yet
				// written its record is another owner until it does.
				s.report(Status{Owner: owner})
				s.sleep(ctx, poll)
				continue
			}
			if err := s.replace(ctx, owner); err != nil {
				s.report(Status{Owner: owner, Err: err})
				s.sleep(ctx, poll)
			}
			// Read the lock again: another collector may have taken it.
			continue
		}
		started := s.clock.Now()
		err = s.supervise(ctx)
		if ctx.Err() != nil {
			return err
		}
		if s.clock.Now().Sub(started) >= healthyRun {
			step = 0
		}
		s.report(Status{Err: err})
		s.sleep(ctx, backoff[min(step, len(backoff)-1)])
		step++
	}
	return nil
}

// replace terminates an orphaned app collector and waits for the lock to free.
func (s *Supervisor) replace(ctx context.Context, orphan collector.Owner) error {
	terminate := s.Terminate
	if terminate == nil {
		terminate = TerminateProcess
	}
	if err := terminate(orphan.PID); err != nil {
		return fmt.Errorf("stop orphaned collector %d: %w", orphan.PID, err)
	}
	for waited := time.Duration(0); waited < orphanWait; waited += orphanPoll {
		if !s.sleep(ctx, orphanPoll) {
			return ctx.Err()
		}
		owner, held, err := s.ReadOwner()
		if err != nil {
			return fmt.Errorf("read collector lock: %w", err)
		}
		if !held || owner != orphan {
			return nil
		}
	}
	return fmt.Errorf("orphaned collector %d did not exit", orphan.PID)
}

// supervise runs one child until it exits or ctx is cancelled. It returns why
// the child exited, or the error stopping it.
func (s *Supervisor) supervise(ctx context.Context) error {
	child, err := s.Start(ctx)
	if err != nil {
		return fmt.Errorf("start collector: %w", err)
	}
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		r := child.Notifications()
		if r == nil {
			return
		}
		if s.Notify != nil {
			_ = attention.ReadNotifications(r, s.Notify)
		}
		// Read whatever is left so the child never blocks writing to a full
		// pipe and the pipe closes once it ends.
		_, _ = io.Copy(io.Discard, r)
	}()
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()
	readyCtx, cancelReady := context.WithCancel(ctx)
	defer cancelReady()
	ready := make(chan error, 1)
	check := func() {
		go func() {
			if s.Ready == nil {
				ready <- nil
				return
			}
			ready <- s.Ready(readyCtx)
		}()
	}
	check()
	// A child can take long to serve chat, as while it migrates the store:
	// it is asked again with backoff for as long as it runs.
	var retry <-chan time.Time
	attempt := 0
	for {
		select {
		case err := <-ready:
			if err != nil {
				// Not collecting as far as the app is concerned: it opens
				// chat only while Collecting is set.
				s.report(Status{Err: fmt.Errorf("collector not answering: %w", err)})
				retry = s.clock.After(readyBackoff[min(attempt, len(readyBackoff)-1)])
				attempt++
				continue
			}
			s.report(Status{Collecting: true})
		case <-retry:
			retry = nil
			check()
		case err := <-exited:
			<-drained
			if ctx.Err() != nil {
				// Exited as the app quit: a clean shutdown, not a crash.
				return nil
			}
			if err == nil {
				return errors.New("collector exited")
			}
			return fmt.Errorf("collector exited: %w", err)
		case <-ctx.Done():
			err := child.Stop()
			<-exited
			<-drained
			return err
		}
	}
}

// sleep waits for d and reports false if ctx ended first.
func (s *Supervisor) sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-s.clock.After(d):
		return true
	}
}

// report forwards a status unless it repeats the previous one.
func (s *Supervisor) report(st Status) {
	if s.last != nil && sameStatus(*s.last, st) {
		return
	}
	s.last = &st
	if s.Status != nil {
		s.Status(st)
	}
}

func sameStatus(a, b Status) bool {
	if a.Collecting != b.Collecting || a.Owner != b.Owner || (a.Err == nil) != (b.Err == nil) {
		return false
	}
	return a.Err == nil || a.Err.Error() == b.Err.Error()
}

// WaitChat returns once the collector's chat socket in dataDir accepts a
// connection, or after ten seconds with the last dial error. The supervisor
// calls it again while the collector runs.
func WaitChat(ctx context.Context, dataDir string) error {
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	path := chat.SocketPath(dataDir)
	var dialer net.Dialer
	for {
		conn, err := dialer.DialContext(ctx, "unix", path)
		if err == nil {
			return conn.Close()
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(100 * time.Millisecond):
		}
	}
}
