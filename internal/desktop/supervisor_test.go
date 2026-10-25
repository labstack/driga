package desktop

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/labstack/driga/internal/attention"
	"github.com/labstack/driga/internal/collector"
)

// fakeClock fires every timer at once and advances its time by the delay, so
// a test sees the exact sequence of waits without sleeping.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	delays []time.Duration
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	c.delays = append(c.delays, d)
	ch := make(chan time.Time, 1)
	ch <- c.now
	return ch
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *fakeClock) waits() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.delays...)
}

type fakeChild struct {
	exit    chan error
	stopped chan struct{}
	once    sync.Once
	notes   io.Reader
}

func newFakeChild() *fakeChild {
	return &fakeChild{exit: make(chan error, 2), stopped: make(chan struct{})}
}

func (c *fakeChild) Wait() error { return <-c.exit }

func (c *fakeChild) Stop() error {
	c.once.Do(func() {
		close(c.stopped)
		c.exit <- nil
	})
	return nil
}

func (c *fakeChild) Notifications() io.Reader { return c.notes }

type statusLog struct {
	mu   sync.Mutex
	list []Status
}

func (l *statusLog) add(s Status) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.list = append(l.list, s)
}

func (l *statusLog) all() []Status {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Status(nil), l.list...)
}

func newSupervisor(clock *fakeClock, statuses *statusLog) *Supervisor {
	return &Supervisor{
		Executable: "/apps/driga",
		ReadOwner:  func() (collector.Owner, bool, error) { return collector.Owner{}, false, nil },
		Ready:      func(context.Context) error { return nil },
		Notify:     func(attention.Notification) {},
		Status:     statuses.add,
		Terminate:  func(int) error { return errors.New("unexpected terminate") },
		clock:      clock,
	}
}

// waitCollecting waits for the supervisor to report its child ready.
func waitCollecting(t *testing.T, statuses *statusLog) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := statuses.all(); len(got) > 0 && got[len(got)-1].Collecting {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("never collecting: %+v", statuses.all())
}

func runSupervisor(t *testing.T, s *Supervisor, ctx context.Context) chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	return done
}

func wait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
	var zero T
	return zero
}

func TestSupervisorStartsWhenFree(t *testing.T) {
	clock := &fakeClock{}
	statuses := &statusLog{}
	s := newSupervisor(clock, statuses)
	child := newFakeChild()
	started := make(chan struct{})
	s.Start = func(context.Context) (Child, error) {
		close(started)
		return child, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := runSupervisor(t, s, ctx)
	wait(t, started)
	waitCollecting(t, statuses)
	cancel()
	if err := wait(t, done); err != nil {
		t.Fatal(err)
	}
	select {
	case <-child.stopped:
	default:
		t.Fatal("child not stopped on cancel")
	}
	if got := statuses.all(); len(got) == 0 || !got[0].Collecting {
		t.Fatalf("statuses = %+v, want collecting", got)
	}
}

func TestSupervisorReportsCollectingOnlyWhenReady(t *testing.T) {
	clock := &fakeClock{}
	statuses := &statusLog{}
	s := newSupervisor(clock, statuses)
	release := make(chan struct{})
	s.Ready = func(ctx context.Context) error {
		<-release
		return nil
	}
	child := newFakeChild()
	started := make(chan struct{})
	s.Start = func(context.Context) (Child, error) {
		close(started)
		return child, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runSupervisor(t, s, ctx)
	wait(t, started)
	time.Sleep(20 * time.Millisecond)
	if got := statuses.all(); len(got) != 0 {
		t.Fatalf("status before ready: %+v", got)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for len(statuses.all()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := statuses.all(); len(got) != 1 || !got[0].Collecting || got[0].Err != nil {
		t.Fatalf("statuses = %+v", got)
	}
	cancel()
	wait(t, done)
}

func TestSupervisorReplacesOwnOrphan(t *testing.T) {
	clock := &fakeClock{}
	statuses := &statusLog{}
	s := newSupervisor(clock, statuses)
	orphan := collector.Owner{PID: 42, Kind: "app", Executable: "/apps/driga"}
	var mu sync.Mutex
	terminated := []int{}
	freeAfter := 3 // reads that still see the orphan after the signal
	s.ReadOwner = func() (collector.Owner, bool, error) {
		mu.Lock()
		defer mu.Unlock()
		if len(terminated) == 0 {
			return orphan, true, nil
		}
		if freeAfter > 0 {
			freeAfter--
			return orphan, true, nil
		}
		return collector.Owner{}, false, nil
	}
	s.Terminate = func(pid int) error {
		mu.Lock()
		defer mu.Unlock()
		terminated = append(terminated, pid)
		return nil
	}
	started := make(chan struct{})
	s.Start = func(context.Context) (Child, error) {
		close(started)
		return newFakeChild(), nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runSupervisor(t, s, ctx)
	wait(t, started)
	cancel()
	wait(t, done)
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(terminated, []int{42}) {
		t.Fatalf("terminated = %v, want [42]", terminated)
	}
}

func TestSupervisorReportsOrphanThatWillNotStop(t *testing.T) {
	clock := &fakeClock{}
	statuses := &statusLog{}
	s := newSupervisor(clock, statuses)
	orphan := collector.Owner{PID: 42, Kind: "app", Executable: "/apps/driga"}
	reads := 0
	s.ReadOwner = func() (collector.Owner, bool, error) {
		reads++
		if reads > 200 {
			return collector.Owner{}, false, nil
		}
		return orphan, true, nil
	}
	s.Terminate = func(int) error { return nil }
	started := make(chan struct{})
	s.Start = func(context.Context) (Child, error) {
		close(started)
		return newFakeChild(), nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runSupervisor(t, s, ctx)
	wait(t, started)
	cancel()
	wait(t, done)
	var reported bool
	for _, st := range statuses.all() {
		if st.Err != nil && st.Owner == orphan {
			reported = true
		}
	}
	if !reported {
		t.Fatalf("stuck orphan not reported: %+v", statuses.all())
	}
}

func TestSupervisorTakesOverWhenOtherOwnerExits(t *testing.T) {
	for _, owner := range []collector.Owner{
		{PID: 7, Kind: "service", Executable: "/usr/local/bin/driga"},
		{PID: 8, Kind: "manual", Executable: "/apps/driga"},
		{PID: 9, Kind: "app", Executable: "/other/driga"},
		{Kind: "unknown"},
	} {
		t.Run(owner.Kind, func(t *testing.T) {
			clock := &fakeClock{}
			statuses := &statusLog{}
			s := newSupervisor(clock, statuses)
			s.Poll = 3 * time.Second
			reads := 0
			s.ReadOwner = func() (collector.Owner, bool, error) {
				reads++
				if reads <= 3 {
					return owner, true, nil
				}
				return collector.Owner{}, false, nil
			}
			started := make(chan struct{})
			s.Start = func(context.Context) (Child, error) {
				close(started)
				return newFakeChild(), nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := runSupervisor(t, s, ctx)
			wait(t, started)
			waitCollecting(t, statuses)
			cancel()
			wait(t, done)
			if got, want := clock.waits(), []time.Duration{3 * time.Second, 3 * time.Second, 3 * time.Second}; !reflect.DeepEqual(got, want) {
				t.Fatalf("waits = %v, want %v", got, want)
			}
			got := statuses.all()
			if len(got) < 2 || got[0] != (Status{Owner: owner}) || !got[len(got)-1].Collecting {
				t.Fatalf("statuses = %+v", got)
			}
			for _, st := range got[:len(got)-1] {
				if st.Collecting {
					t.Fatalf("collecting while another owner holds the lock: %+v", got)
				}
			}
		})
	}
}

func TestSupervisorBacksOffOnRepeatedExits(t *testing.T) {
	clock := &fakeClock{}
	statuses := &statusLog{}
	s := newSupervisor(clock, statuses)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	starts := 0
	crash := errors.New("exit status 1")
	execFailed := errors.New("exec failed")
	s.Start = func(context.Context) (Child, error) {
		starts++
		switch {
		case starts == 4:
			return nil, execFailed
		case starts == 8:
			cancel()
			return newFakeChild(), nil
		}
		child := newFakeChild()
		child.exit <- crash
		return child, nil
	}
	done := runSupervisor(t, s, ctx)
	wait(t, done)
	want := []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, 30 * time.Second, 30 * time.Second}
	if got := clock.waits(); !reflect.DeepEqual(got, want) {
		t.Fatalf("waits = %v, want %v", got, want)
	}
	var errs int
	for _, st := range statuses.all() {
		if st.Err != nil {
			errs++
			if st.Collecting {
				t.Fatalf("crash reported as collecting: %+v", st)
			}
			if !errors.Is(st.Err, crash) && !errors.Is(st.Err, execFailed) {
				t.Fatalf("unexpected error %v", st.Err)
			}
		}
	}
	if errs == 0 {
		t.Fatal("exit error not reported")
	}
}

func TestSupervisorResetsBackoffAfterHealthyRun(t *testing.T) {
	clock := &fakeClock{}
	s := newSupervisor(clock, &statusLog{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	starts := 0
	s.Start = func(context.Context) (Child, error) {
		starts++
		child := newFakeChild()
		switch starts {
		case 3:
			// Healthy for five minutes before it fails.
			clock.advance(5 * time.Minute)
		case 5:
			cancel()
			return child, nil
		}
		child.exit <- errors.New("exit status 1")
		return child, nil
	}
	done := runSupervisor(t, s, ctx)
	wait(t, done)
	want := []time.Duration{time.Second, 2 * time.Second, time.Second, 2 * time.Second}
	if got := clock.waits(); !reflect.DeepEqual(got, want) {
		t.Fatalf("waits = %v, want %v", got, want)
	}
}

func TestSupervisorUsesConfiguredBackoff(t *testing.T) {
	clock := &fakeClock{}
	s := newSupervisor(clock, &statusLog{})
	s.Backoff = []time.Duration{time.Millisecond, 3 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	starts := 0
	s.Start = func(context.Context) (Child, error) {
		starts++
		if starts == 4 {
			cancel()
		}
		return nil, errors.New("exec failed")
	}
	wait(t, runSupervisor(t, s, ctx))
	want := []time.Duration{time.Millisecond, 3 * time.Millisecond, 3 * time.Millisecond}
	if got := clock.waits(); !reflect.DeepEqual(got, want) {
		t.Fatalf("waits = %v, want %v", got, want)
	}
}

func TestSupervisorForwardsNotifications(t *testing.T) {
	clock := &fakeClock{}
	s := newSupervisor(clock, &statusLog{})
	want := []attention.Notification{
		{Kind: "waiting", Title: "Needs you", ItemID: "a"},
		{Kind: "finished", Title: "Finished", ItemID: "b"},
	}
	var buf bytes.Buffer
	pipe := &attention.PipeNotifier{W: &buf}
	for _, n := range want {
		if err := pipe.Notify(context.Background(), n); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var got []attention.Notification
	s.Notify = func(n attention.Notification) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, n)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start = func(context.Context) (Child, error) {
		child := newFakeChild()
		child.notes = &buf
		// The child exits after writing; the supervisor must still deliver
		// everything it wrote before restarting.
		child.exit <- errors.New("exit status 1")
		cancel()
		return child, nil
	}
	wait(t, runSupervisor(t, s, ctx))
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("notifications = %+v, want %+v", got, want)
	}
}

func TestSupervisorReportsReadOwnerError(t *testing.T) {
	clock := &fakeClock{}
	statuses := &statusLog{}
	s := newSupervisor(clock, statuses)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reads := 0
	boom := errors.New("permission denied")
	s.ReadOwner = func() (collector.Owner, bool, error) {
		reads++
		if reads == 3 {
			cancel()
		}
		return collector.Owner{}, false, boom
	}
	s.Start = func(context.Context) (Child, error) {
		t.Error("started despite unreadable lock")
		return newFakeChild(), nil
	}
	wait(t, runSupervisor(t, s, ctx))
	got := statuses.all()
	if len(got) != 1 || !errors.Is(got[0].Err, boom) {
		t.Fatalf("statuses = %+v, want one deduplicated error", got)
	}
}

// eofReader records whether its stream was read to the end.
type eofReader struct {
	r    io.Reader
	mu   sync.Mutex
	done bool
}

func (e *eofReader) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err == io.EOF {
		e.mu.Lock()
		e.done = true
		e.mu.Unlock()
	}
	return n, err
}

func (e *eofReader) drained() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.done
}

func TestSupervisorDrainsNotifications(t *testing.T) {
	for _, tt := range []struct {
		name   string
		notify bool
	}{{"notify", true}, {"no notify", false}} {
		t.Run(tt.name, func(t *testing.T) {
			s := newSupervisor(&fakeClock{}, &statusLog{})
			var got []string
			s.Notify = nil
			if tt.notify {
				s.Notify = func(n attention.Notification) { got = append(got, n.ItemID) }
			}
			notes := &eofReader{r: bytes.NewReader([]byte("garbage\n{\"item_id\":\"a\"}\n" + string(bytes.Repeat([]byte("x"), 1<<16))))}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s.Start = func(context.Context) (Child, error) {
				child := newFakeChild()
				child.notes = notes
				child.exit <- errors.New("exit status 1")
				cancel()
				return child, nil
			}
			wait(t, runSupervisor(t, s, ctx))
			if !notes.drained() {
				t.Fatal("notification stream not read to the end")
			}
			if tt.notify && !reflect.DeepEqual(got, []string{"a"}) {
				t.Fatalf("delivered %v, want [a]", got)
			}
		})
	}
}

// A child that exits just as the app quits is a clean shutdown, not a crash.
func TestSupervisorShutdownRaceReturnsStopResult(t *testing.T) {
	for range 100 {
		s := newSupervisor(&fakeClock{}, &statusLog{})
		ctx, cancel := context.WithCancel(context.Background())
		s.Start = func(context.Context) (Child, error) {
			child := newFakeChild()
			child.exit <- errors.New("signal: terminated")
			cancel()
			return child, nil
		}
		if err := wait(t, runSupervisor(t, s, ctx)); err != nil {
			t.Fatalf("Run = %v, want nil on shutdown", err)
		}
	}
}

func TestSupervisorRechecksOwnerAfterOrphanExits(t *testing.T) {
	clock := &fakeClock{}
	statuses := &statusLog{}
	s := newSupervisor(clock, statuses)
	orphan := collector.Owner{PID: 42, Kind: "app", Executable: "/apps/driga"}
	service := collector.Owner{PID: 7, Kind: "service", Executable: "/usr/local/bin/driga"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	terminated := false
	reads := 0
	s.Terminate = func(int) error {
		terminated = true
		return nil
	}
	s.ReadOwner = func() (collector.Owner, bool, error) {
		reads++
		switch {
		case !terminated:
			return orphan, true, nil
		case reads > 6:
			cancel()
		}
		return service, true, nil
	}
	s.Start = func(context.Context) (Child, error) {
		t.Error("started while the service holds the lock")
		return newFakeChild(), nil
	}
	wait(t, runSupervisor(t, s, ctx))
	if got := statuses.all(); len(got) == 0 || got[len(got)-1] != (Status{Owner: service}) {
		t.Fatalf("statuses = %+v, want the service reported", got)
	}
}

func TestSupervisorReadyFailureIsNotCollecting(t *testing.T) {
	statuses := &statusLog{}
	s := newSupervisor(&fakeClock{}, statuses)
	s.Ready = func(context.Context) error { return errors.New("connection refused") }
	started := make(chan struct{})
	s.Start = func(context.Context) (Child, error) {
		close(started)
		return newFakeChild(), nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := runSupervisor(t, s, ctx)
	wait(t, started)
	deadline := time.Now().Add(5 * time.Second)
	for len(statuses.all()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	wait(t, done)
	got := statuses.all()
	if len(got) != 1 || got[0].Collecting || got[0].Err == nil {
		t.Fatalf("statuses = %+v, want not collecting with an error", got)
	}
}
