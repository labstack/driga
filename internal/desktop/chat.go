package desktop

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"sync"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/store"
)

type collectorRemote interface {
	chat.Client
	chat.PromptClient
	Close()
}

// collectorChat reaches the chat broker of whichever collector runs, dialing
// it on first use and again while it does not answer. It never falls back to
// a private broker: chat waits for the collector instead.
type collectorChat struct {
	dataDir    string
	mu         sync.Mutex
	remote     collectorRemote
	dial       func(context.Context, string) (collectorRemote, error)
	dialing    chan struct{}
	cancelDial context.CancelFunc
	closed     bool
}

func (c *collectorChat) client(ctx context.Context) (collectorRemote, error) {
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
		var r collectorRemote
		var err error
		if dial != nil {
			r, err = dial(dialCtx, c.dataDir)
		} else {
			var remote *chat.Remote
			remote, err = chat.Dial(dialCtx, c.dataDir)
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
// cannot install a new connection after the app has closed.
func (c *collectorChat) Close() {
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
func (c *collectorChat) failed(r collectorRemote, err error) {
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

func (c *collectorChat) Create(ctx context.Context) (store.Conversation, error) {
	r, err := c.client(ctx)
	if err != nil {
		return store.Conversation{}, err
	}
	out, err := r.Create(ctx)
	c.failed(r, err)
	return out, err
}

func (c *collectorChat) List(ctx context.Context) ([]store.Conversation, error) {
	r, err := c.client(ctx)
	if err != nil {
		return nil, err
	}
	out, err := r.List(ctx)
	c.failed(r, err)
	return out, err
}

func (c *collectorChat) Send(ctx context.Context, id, text string, scope chat.Context) error {
	r, err := c.client(ctx)
	if err != nil {
		return err
	}
	err = r.Send(ctx, id, text, scope)
	c.failed(r, err)
	return err
}

func (c *collectorChat) Stop(ctx context.Context, id string) error {
	r, err := c.client(ctx)
	if err != nil {
		return err
	}
	err = r.Stop(ctx, id)
	c.failed(r, err)
	return err
}

func (c *collectorChat) SetMode(ctx context.Context, id string, mode chat.Mode) error {
	r, err := c.client(ctx)
	if err != nil {
		return err
	}
	err = r.SetMode(ctx, id, mode)
	c.failed(r, err)
	return err
}

func (c *collectorChat) Decide(ctx context.Context, id, action string, allow bool) error {
	r, err := c.client(ctx)
	if err != nil {
		return err
	}
	err = r.Decide(ctx, id, action, allow)
	c.failed(r, err)
	return err
}

func (c *collectorChat) Read(ctx context.Context, id string, after uint64) (chat.State, error) {
	r, err := c.client(ctx)
	if err != nil {
		return chat.State{}, err
	}
	out, err := r.Read(ctx, id, after)
	c.failed(r, err)
	return out, err
}

func (c *collectorChat) Wait(ctx context.Context, id string, after uint64) (chat.State, error) {
	r, err := c.client(ctx)
	if err != nil {
		return chat.State{}, err
	}
	out, err := r.Wait(ctx, id, after)
	c.failed(r, err)
	return out, err
}

func (c *collectorChat) Prompts(ctx context.Context) ([]chat.Prompt, error) {
	r, err := c.client(ctx)
	if err != nil {
		return nil, err
	}
	out, err := r.Prompts(ctx)
	c.failed(r, err)
	return out, err
}

func (c *collectorChat) AnswerPrompt(ctx context.Context, id string, allow bool) error {
	r, err := c.client(ctx)
	if err != nil {
		return err
	}
	err = r.AnswerPrompt(ctx, id, allow)
	c.failed(r, err)
	return err
}

var (
	_ chat.Client       = (*collectorChat)(nil)
	_ chat.PromptClient = (*collectorChat)(nil)
)
