package desktop

import (
	"context"
	"errors"
	"io"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/driga/internal/chat"
)

type fakeCollectorRemote struct {
	chat.Client
	chat.PromptClient
	sends   atomic.Int32
	closes  atomic.Int32
	sendErr error
	close   func()
}

func (r *fakeCollectorRemote) Close() {
	r.closes.Add(1)
	if r.close != nil {
		r.close()
	}
}
func (r *fakeCollectorRemote) Send(context.Context, string, string, chat.Context) error {
	r.sends.Add(1)
	return r.sendErr
}
func connectionResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatal("connection lifecycle blocked")
		return nil
	}
}

func TestCollectorChatCloseDuringDial(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	remote := &fakeCollectorRemote{}
	var dials atomic.Int32
	c := &collectorChat{dial: func(context.Context, string) (collectorRemote, error) {
		dials.Add(1)
		close(started)
		<-release
		return remote, nil
	}}
	result := make(chan error, 1)
	go func() { _, err := c.client(context.Background()); result <- err }()
	<-started
	closed := make(chan error, 1)
	go func() { c.Close(); closed <- nil }()
	if err := connectionResult(t, closed); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := connectionResult(t, result); err == nil {
		t.Fatal("pending dial installed a connection after Close")
	}
	if remote.closes.Load() != 1 {
		t.Fatalf("discarded connection closed %d times", remote.closes.Load())
	}
	if _, err := c.client(context.Background()); err == nil {
		t.Fatal("closed client dialled again")
	}
	if dials.Load() != 1 {
		t.Fatalf("closed client dialled %d times", dials.Load())
	}
}

func TestCollectorChatCloseDoesNotHoldMutex(t *testing.T) {
	c := &collectorChat{}
	remote := &fakeCollectorRemote{close: func() { c.mu.Lock(); c.mu.Unlock() }}
	c.remote = remote
	done := make(chan error, 1)
	go func() { c.Close(); done <- nil }()
	if err := connectionResult(t, done); err != nil {
		t.Fatal(err)
	}
}

func TestCollectorChatRedialsAfterTransportFailureWithoutReplay(t *testing.T) {
	unavailable := &url.Error{Op: "Post", URL: "http://driga/send", Err: io.EOF}
	first, next := &fakeCollectorRemote{sendErr: unavailable}, &fakeCollectorRemote{}
	var dials atomic.Int32
	c := &collectorChat{dial: func(context.Context, string) (collectorRemote, error) {
		if dials.Add(1) == 1 {
			return first, nil
		}
		return next, nil
	}}
	defer c.Close()
	if err := c.Send(context.Background(), "id", "one", chat.Context{}); !errors.Is(err, io.EOF) {
		t.Fatalf("first send: %v", err)
	}
	if dials.Load() != 1 || first.sends.Load() != 1 || next.sends.Load() != 0 || first.closes.Load() != 1 {
		t.Fatal("failed state-changing request replayed or unavailable transport retained")
	}
	if err := c.Send(context.Background(), "id", "two", chat.Context{}); err != nil {
		t.Fatal(err)
	}
	if dials.Load() != 2 || first.sends.Load() != 1 || next.sends.Load() != 1 {
		t.Fatal("next request did not redial exactly once")
	}
}

func TestCollectorChatKeepsTransportForApplicationErrors(t *testing.T) {
	for _, err := range []error{errors.New("chat broker: HTTP 400: invalid conversation"), context.Canceled, context.DeadlineExceeded} {
		t.Run(err.Error(), func(t *testing.T) {
			remote := &fakeCollectorRemote{sendErr: err}
			dials := 0
			c := &collectorChat{dial: func(context.Context, string) (collectorRemote, error) { dials++; return remote, nil }}
			defer c.Close()
			for range 2 {
				if got := c.Send(context.Background(), "id", "text", chat.Context{}); !errors.Is(got, err) {
					t.Fatalf("send: %v", got)
				}
			}
			if dials != 1 || remote.closes.Load() != 0 {
				t.Fatal("application error or cancelled poll dropped live connection")
			}
		})
	}
}

func TestCollectorChatOldFailureKeepsNewConnection(t *testing.T) {
	old, current := &fakeCollectorRemote{}, &fakeCollectorRemote{}
	c := &collectorChat{remote: current}
	defer c.Close()
	c.failed(old, &url.Error{Op: "Get", URL: "http://driga/state", Err: io.EOF})
	if c.remote != current || current.closes.Load() != 0 {
		t.Fatal("old request discarded a replacement connection")
	}
}

func TestCollectorChatDialWaiterCanCancel(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	remote := &fakeCollectorRemote{}
	var dials atomic.Int32
	c := &collectorChat{dial: func(context.Context, string) (collectorRemote, error) {
		dials.Add(1)
		close(started)
		<-release
		return remote, nil
	}}
	leader := make(chan error, 1)
	go func() { _, err := c.client(context.Background()); leader <- err }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	waiter := make(chan error, 1)
	go func() { _, err := c.client(ctx); waiter <- err }()
	if err := connectionResult(t, waiter); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting caller: %v", err)
	}
	close(release)
	if err := connectionResult(t, leader); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if dials.Load() != 1 {
		t.Fatalf("concurrent callers dialled %d times", dials.Load())
	}
}

func TestCollectorChatCloseCancelsPendingDial(t *testing.T) {
	started := make(chan struct{})
	c := &collectorChat{dial: func(ctx context.Context, _ string) (collectorRemote, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := c.client(ctx); result <- err }()
	<-started
	done := make(chan error, 1)
	go func() { c.Close(); done <- nil }()
	if err := connectionResult(t, done); err != nil {
		t.Fatal(err)
	}
	if err := connectionResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("pending dial: %v", err)
	}
}
