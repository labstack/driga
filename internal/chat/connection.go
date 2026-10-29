package chat

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"sync"

	"github.com/labstack/driga/internal/store"
)

type connectionRemote interface {
	Client
	PromptClient
	Close()
}

// Connection reaches the collector broker lazily and replaces unavailable
// transports for the next operation. Failed mutations are never replayed.
type Connection struct {
	dataDir    string
	mu         sync.Mutex
	remote     connectionRemote
	dial       func(context.Context, string) (connectionRemote, error)
	dialing    chan struct{}
	cancelDial context.CancelFunc
	closed     bool
}

// NewConnection provides collector-only Chat. It never creates a private broker.
func NewConnection(dataDir string) *Connection {
	return &Connection{dataDir: dataDir}
}

func (c *Connection) client(ctx context.Context) (connectionRemote, error) {
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return nil, errors.New("Chat client closed")
		}
		if c.remote != nil {
			r := c.remote
			c.mu.Unlock()
			return r, nil
		}
		if done := c.dialing; done != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-done:
				continue
			}
		}
		dialCtx, cancel := context.WithCancel(ctx)
		c.dialing, c.cancelDial = make(chan struct{}), cancel
		dial := c.dial
		c.mu.Unlock()
		var r connectionRemote
		var err error
		if dial != nil {
			r, err = dial(dialCtx, c.dataDir)
		} else {
			var remote *Remote
			remote, err = Dial(dialCtx, c.dataDir)
			if remote != nil {
				r = remote
			}
		}
		cancel()
		c.mu.Lock()
		if c.closed && err == nil {
			err = errors.New("Chat client closed")
		}
		if err == nil {
			c.remote = r
		}
		close(c.dialing)
		c.dialing, c.cancelDial = nil, nil
		c.mu.Unlock()
		if err != nil {
			if r != nil {
				r.Close()
			}
			return nil, err
		}
		return r, nil
	}
}

// Close ends what this client turned on, such as full auto. A pending dial
// cannot install a new connection after the client has closed.
func (c *Connection) Close() {
	c.mu.Lock()
	c.closed = true
	r := c.remote
	c.remote = nil
	cancel := c.cancelDial
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if r != nil {
		r.Close()
	}
}

// failed forgets an unavailable transport for the next request. The failed
// request is never retried: it may already have changed state at the service.
func (c *Connection) failed(r connectionRemote, err error) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	var transport *url.Error
	var key *os.PathError
	if !errors.As(err, &transport) && !errors.As(err, &key) && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return
	}
	c.mu.Lock()
	forget := c.remote == r
	if forget {
		c.remote = nil
	}
	c.mu.Unlock()
	if forget {
		r.Close()
	}
}

func (c *Connection) Create(ctx context.Context) (store.Conversation, error) {
	r, err := c.client(ctx)
	if err != nil {
		return store.Conversation{}, err
	}
	out, err := r.Create(ctx)
	c.failed(r, err)
	return out, err
}

func (c *Connection) List(ctx context.Context) ([]store.Conversation, error) {
	r, err := c.client(ctx)
	if err != nil {
		return nil, err
	}
	out, err := r.List(ctx)
	c.failed(r, err)
	return out, err
}

func (c *Connection) Send(ctx context.Context, id, text string, scope Context) error {
	r, err := c.client(ctx)
	if err != nil {
		return err
	}
	err = r.Send(ctx, id, text, scope)
	c.failed(r, err)
	return err
}

func (c *Connection) Stop(ctx context.Context, id string) error {
	r, err := c.client(ctx)
	if err != nil {
		return err
	}
	err = r.Stop(ctx, id)
	c.failed(r, err)
	return err
}

func (c *Connection) SetMode(ctx context.Context, id string, mode Mode) error {
	r, err := c.client(ctx)
	if err != nil {
		return err
	}
	err = r.SetMode(ctx, id, mode)
	c.failed(r, err)
	return err
}

func (c *Connection) Decide(ctx context.Context, id, action string, allow bool) error {
	r, err := c.client(ctx)
	if err != nil {
		return err
	}
	err = r.Decide(ctx, id, action, allow)
	c.failed(r, err)
	return err
}

func (c *Connection) Read(ctx context.Context, id string, after uint64) (State, error) {
	r, err := c.client(ctx)
	if err != nil {
		return State{}, err
	}
	out, err := r.Read(ctx, id, after)
	c.failed(r, err)
	return out, err
}

func (c *Connection) Wait(ctx context.Context, id string, after uint64) (State, error) {
	r, err := c.client(ctx)
	if err != nil {
		return State{}, err
	}
	out, err := r.Wait(ctx, id, after)
	c.failed(r, err)
	return out, err
}

func (c *Connection) Prompts(ctx context.Context) ([]Prompt, error) {
	r, err := c.client(ctx)
	if err != nil {
		return nil, err
	}
	out, err := r.Prompts(ctx)
	c.failed(r, err)
	return out, err
}

func (c *Connection) AnswerPrompt(ctx context.Context, id string, allow bool) error {
	r, err := c.client(ctx)
	if err != nil {
		return err
	}
	err = r.AnswerPrompt(ctx, id, allow)
	c.failed(r, err)
	return err
}

var (
	_ Client       = (*Connection)(nil)
	_ PromptClient = (*Connection)(nil)
)

func (c *Connection) DecideSimilar(ctx context.Context, conversation, action string) error {
	r, err := c.client(ctx)
	if err != nil {
		return err
	}
	g, ok := r.(GrantClient)
	if !ok {
		return errors.New("Approval grants unavailable")
	}
	err = g.DecideSimilar(ctx, conversation, action)
	c.failed(r, err)
	return err
}

func (c *Connection) AnswerPromptSimilar(ctx context.Context, id string) error {
	r, err := c.client(ctx)
	if err != nil {
		return err
	}
	g, ok := r.(GrantClient)
	if !ok {
		return errors.New("Approval grants unavailable")
	}
	err = g.AnswerPromptSimilar(ctx, id)
	c.failed(r, err)
	return err
}

func (c *Connection) RevokeGrant(ctx context.Context, id string) error {
	r, err := c.client(ctx)
	if err != nil {
		return err
	}
	g, ok := r.(GrantClient)
	if !ok {
		return errors.New("Approval grants unavailable")
	}
	err = g.RevokeGrant(ctx, id)
	c.failed(r, err)
	return err
}

func (c *Connection) GrantHistory(ctx context.Context) ([]GrantDecision, error) {
	r, err := c.client(ctx)
	if err != nil {
		return nil, err
	}
	g, ok := r.(GrantClient)
	if !ok {
		return nil, errors.New("Approval grants unavailable")
	}
	out, err := g.GrantHistory(ctx)
	c.failed(r, err)
	return out, err
}

var _ GrantClient = (*Connection)(nil)

func (c *Connection) PromptBursts(ctx context.Context) ([]PromptBurst, error) {
	r, err := c.client(ctx)
	if err != nil {
		return nil, err
	}
	g, ok := r.(BurstClient)
	if !ok {
		return nil, errors.New("Prompt bursts unavailable")
	}
	out, err := g.PromptBursts(ctx)
	c.failed(r, err)
	return out, err
}
func (c *Connection) DecideBurst(ctx context.Context, id string, members []string, allow bool) error {
	r, err := c.client(ctx)
	if err != nil {
		return err
	}
	g, ok := r.(BurstClient)
	if !ok {
		return errors.New("Prompt bursts unavailable")
	}
	err = g.DecideBurst(ctx, id, members, allow)
	c.failed(r, err)
	return err
}

var _ BurstClient = (*Connection)(nil)
