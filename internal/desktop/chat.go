package desktop

import (
	"context"
	"sync"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/store"
)

// collectorChat reaches the chat broker of whichever collector runs, dialing
// it on first use and again while it does not answer. It never falls back to
// a private broker: chat waits for the collector instead.
type collectorChat struct {
	dataDir string
	mu      sync.Mutex
	remote  *chat.Remote
}

func (c *collectorChat) client(ctx context.Context) (*chat.Remote, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.remote == nil {
		remote, err := chat.Dial(ctx, c.dataDir)
		if err != nil {
			return nil, err
		}
		c.remote = remote
	}
	return c.remote, nil
}

// Close ends what this client turned on, such as full auto.
func (c *collectorChat) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.remote != nil {
		c.remote.Close()
	}
}

func (c *collectorChat) Create(ctx context.Context) (store.Conversation, error) {
	r, err := c.client(ctx)
	if err != nil {
		return store.Conversation{}, err
	}
	return r.Create(ctx)
}

func (c *collectorChat) List(ctx context.Context) ([]store.Conversation, error) {
	r, err := c.client(ctx)
	if err != nil {
		return nil, err
	}
	return r.List(ctx)
}

func (c *collectorChat) Send(ctx context.Context, id, text string, scope chat.Context) error {
	r, err := c.client(ctx)
	if err != nil {
		return err
	}
	return r.Send(ctx, id, text, scope)
}

func (c *collectorChat) Stop(ctx context.Context, id string) error {
	r, err := c.client(ctx)
	if err != nil {
		return err
	}
	return r.Stop(ctx, id)
}

func (c *collectorChat) SetMode(ctx context.Context, id string, mode chat.Mode) error {
	r, err := c.client(ctx)
	if err != nil {
		return err
	}
	return r.SetMode(ctx, id, mode)
}

func (c *collectorChat) Decide(ctx context.Context, id, action string, allow bool) error {
	r, err := c.client(ctx)
	if err != nil {
		return err
	}
	return r.Decide(ctx, id, action, allow)
}

func (c *collectorChat) Read(ctx context.Context, id string, after uint64) (chat.State, error) {
	r, err := c.client(ctx)
	if err != nil {
		return chat.State{}, err
	}
	return r.Read(ctx, id, after)
}

func (c *collectorChat) Wait(ctx context.Context, id string, after uint64) (chat.State, error) {
	r, err := c.client(ctx)
	if err != nil {
		return chat.State{}, err
	}
	return r.Wait(ctx, id, after)
}

func (c *collectorChat) Prompts(ctx context.Context) ([]chat.Prompt, error) {
	r, err := c.client(ctx)
	if err != nil {
		return nil, err
	}
	return r.Prompts(ctx)
}

func (c *collectorChat) AnswerPrompt(ctx context.Context, id string, allow bool) error {
	r, err := c.client(ctx)
	if err != nil {
		return err
	}
	return r.AnswerPrompt(ctx, id, allow)
}

var (
	_ chat.Client       = (*collectorChat)(nil)
	_ chat.PromptClient = (*collectorChat)(nil)
)
