//go:build !windows

package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func notifyContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
}
