package attention

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
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

// ReadNotifications calls fn for each JSON line until r ends. A malformed
// line is logged and skipped, so one bad write cannot stall the writer or
// hide the notifications after it. It returns nil at EOF.
func ReadNotifications(r io.Reader, fn func(Notification)) error {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 {
			var n Notification
			if jsonErr := json.Unmarshal(line, &n); jsonErr != nil {
				slog.Warn("malformed notification", "event", "notification_malformed", "err", jsonErr.Error())
			} else {
				fn(n)
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
