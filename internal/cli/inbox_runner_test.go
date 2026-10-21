package cli

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/labstack/driga/internal/inbox"
)

type unavailableInboxRunner struct{}

func (unavailableInboxRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return nil, exec.ErrNotFound
}

// All CLI snapshot tests exercise collector reads with a fake command runner.
// They must never query a real agent CLI, tmux server or notification centre.
func TestMain(m *testing.M) {
	newInboxRunner = func() inbox.Runner { return unavailableInboxRunner{} }
	nativeToast = func(context.Context, string) error { return exec.ErrNotFound }
	os.Exit(m.Run())
}
