package chat

import (
	"context"
	"errors"
	"io"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

type fakeConnectionRemote struct {
	Client
	PromptClient
	sends   atomic.Int32
	closes  atomic.Int32
	sendErr error
	send    func(int32) error
	close   func()
}

func (r *fakeConnectionRemote) Close() {
	r.closes.Add(1)
	if r.close != nil {
		r.close()
	}
}
func (r *fakeConnectionRemote) Send(context.Context, string, string, Context) error {
	n := r.sends.Add(1)
	if r.send != nil {
		return r.send(n)
	}
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

func TestConnectionCloseDuringDial(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	remote := &fakeConnectionRemote{}
	var dials atomic.Int32
	c := &Connection{dial: func(context.Context, string) (connectionRemote, error) {
		dials.Add(1)
		close(started)
		<-release
		return remote, nil
	}}
	result := make(chan error, 1)
	go func() { result <- c.Send(context.Background(), "id", "text", Context{}) }()
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
	if err := c.Send(context.Background(), "id", "text", Context{}); err == nil {
		t.Fatal("closed client dialled again")
	}
	if dials.Load() != 1 {
		t.Fatalf("closed client dialled %d times", dials.Load())
	}
}

func TestConnectionRedialsAfterTransportFailureWithoutReplay(t *testing.T) {
	unavailable := &url.Error{Op: "Post", URL: "http://driga/send", Err: io.EOF}
	first, next := &fakeConnectionRemote{sendErr: unavailable}, &fakeConnectionRemote{}
	var dials atomic.Int32
	c := &Connection{dial: func(context.Context, string) (connectionRemote, error) {
		if dials.Add(1) == 1 {
			return first, nil
		}
		return next, nil
	}}
	defer c.Close()
	if err := c.Send(context.Background(), "id", "one", Context{}); !errors.Is(err, io.EOF) {
		t.Fatalf("first send: %v", err)
	}
	if dials.Load() != 1 || first.sends.Load() != 1 || next.sends.Load() != 0 || first.closes.Load() != 1 {
		t.Fatal("failed state-changing request replayed or unavailable transport retained")
	}
	if err := c.Send(context.Background(), "id", "two", Context{}); err != nil {
		t.Fatal(err)
	}
	if dials.Load() != 2 || first.sends.Load() != 1 || next.sends.Load() != 1 {
		t.Fatal("next request did not redial exactly once")
	}
}

func TestConnectionKeepsTransportForApplicationErrors(t *testing.T) {
	for _, err := range []error{errors.New("chat broker: HTTP 400: invalid conversation"), context.Canceled, context.DeadlineExceeded} {
		t.Run(err.Error(), func(t *testing.T) {
			remote := &fakeConnectionRemote{sendErr: err}
			dials := 0
			c := &Connection{dial: func(context.Context, string) (connectionRemote, error) { dials++; return remote, nil }}
			defer c.Close()
			for range 2 {
				if got := c.Send(context.Background(), "id", "text", Context{}); !errors.Is(got, err) {
					t.Fatalf("send: %v", got)
				}
			}
			if dials != 1 || remote.closes.Load() != 0 {
				t.Fatal("application error or cancelled poll dropped live connection")
			}
		})
	}
}

func TestConnectionDialWaiterCanCancel(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	remote := &fakeConnectionRemote{}
	var dials atomic.Int32
	c := &Connection{dial: func(context.Context, string) (connectionRemote, error) {
		dials.Add(1)
		close(started)
		<-release
		return remote, nil
	}}
	leader := make(chan error, 1)
	go func() { leader <- c.Send(context.Background(), "id", "text", Context{}) }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	waiter := make(chan error, 1)
	go func() { waiter <- c.Send(ctx, "id", "text", Context{}) }()
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

func TestConnectionCloseCancelsPendingDial(t *testing.T) {
	started := make(chan struct{})
	c := &Connection{dial: func(ctx context.Context, _ string) (connectionRemote, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- c.Send(ctx, "id", "text", Context{}) }()
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

func TestConnectionOldFailureKeepsNewConnection(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	old := &fakeConnectionRemote{send: func(n int32) error {
		if n == 1 {
			close(started)
			<-release
		}
		return io.EOF
	}}
	next := &fakeConnectionRemote{}
	dials := 0
	c := &Connection{dial: func(context.Context, string) (connectionRemote, error) {
		dials++
		if dials == 1 {
			return old, nil
		}
		return next, nil
	}}
	defer c.Close()
	result := make(chan error, 1)
	go func() { result <- c.Send(context.Background(), "id", "old", Context{}) }()
	<-started
	if err := c.Send(context.Background(), "id", "failed", Context{}); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if err := c.Send(context.Background(), "id", "replacement", Context{}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := connectionResult(t, result); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if err := c.Send(context.Background(), "id", "still replacement", Context{}); err != nil {
		t.Fatal(err)
	}
	if dials != 2 || next.sends.Load() != 2 || next.closes.Load() != 0 || old.closes.Load() != 1 {
		t.Fatal("old request discarded a replacement connection")
	}
}

func TestConnectionCloseAllowsClientReentry(t *testing.T) {
	var c *Connection
	remote := &fakeConnectionRemote{close: func() {
		if err := c.Send(context.Background(), "id", "after close", Context{}); err == nil {
			t.Error("closed connection accepted a send")
		}
	}}
	c = &Connection{dial: func(context.Context, string) (connectionRemote, error) { return remote, nil }}
	if err := c.Send(context.Background(), "id", "before close", Context{}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { c.Close(); done <- nil }()
	if err := connectionResult(t, done); err != nil {
		t.Fatal(err)
	}
}

type grantRemote struct {
	fakeConnectionRemote
	calls int
}

func (r *grantRemote) DecideSimilar(context.Context, string, string) error { r.calls++; return io.EOF }
func (r *grantRemote) AnswerPromptSimilar(context.Context, string) error   { r.calls++; return io.EOF }
func (r *grantRemote) RevokeGrant(context.Context, string) error           { r.calls++; return io.EOF }
func (r *grantRemote) GrantHistory(context.Context) ([]GrantDecision, error) {
	return []GrantDecision{{GrantID: "g"}}, nil
}
func TestGrantConnectionForwarding(t *testing.T) {
	var _ GrantClient = (*Connection)(nil)
	r := &grantRemote{}
	c := &Connection{dial: func(context.Context, string) (connectionRemote, error) { return r, nil }}
	defer c.Close()
	if err := c.DecideSimilar(context.Background(), "c", "a"); !errors.Is(err, io.EOF) || r.calls != 1 {
		t.Fatal(err, r.calls)
	}
	old := &Connection{dial: func(context.Context, string) (connectionRemote, error) { return &fakeConnectionRemote{}, nil }}
	defer old.Close()
	if err := old.AnswerPromptSimilar(context.Background(), "p"); err == nil {
		t.Fatal("unsupported peer accepted")
	}
}

type burstRemote struct {
	fakeConnectionRemote
	calls int
}

func (r *burstRemote) PromptBursts(context.Context) ([]PromptBurst, error) {
	return []PromptBurst{{ID: "b"}}, nil
}
func (r *burstRemote) DecideBurst(context.Context, string, []string, bool) error {
	r.calls++
	return io.EOF
}
func TestPromptBurstConnection(t *testing.T) {
	var _ BurstClient = (*Connection)(nil)
	r := &burstRemote{}
	c := &Connection{dial: func(context.Context, string) (connectionRemote, error) { return r, nil }}
	defer c.Close()
	groups, err := c.PromptBursts(context.Background())
	if err != nil || len(groups) != 1 {
		t.Fatal(groups, err)
	}
	if err = c.DecideBurst(context.Background(), "b", []string{"p"}, true); !errors.Is(err, io.EOF) || r.calls != 1 {
		t.Fatal(err, r.calls)
	}
	unsupported := &Connection{dial: func(context.Context, string) (connectionRemote, error) { return &fakeConnectionRemote{}, nil }}
	defer unsupported.Close()
	if err = unsupported.DecideBurst(context.Background(), "b", []string{"p"}, true); err == nil {
		t.Fatal("unsupported peer accepted")
	}
}
