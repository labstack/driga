package attention

import (
	"context"
	"encoding/json"
	"io"
	"sync"
)

type PipeNotifier struct {
	W  io.Writer
	mu sync.Mutex
}

func (p *PipeNotifier) Notify(_ context.Context, n Notification) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return json.NewEncoder(p.W).Encode(n)
}

func ReadNotifications(r io.Reader, fn func(Notification)) error {
	decoder := json.NewDecoder(r)
	for {
		var n Notification
		if err := decoder.Decode(&n); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		fn(n)
	}
}
