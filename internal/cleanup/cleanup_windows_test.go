package cleanup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/driga/internal/platform"
)

func TestWindowsCleanupRefusesBeforeIO(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	_, err := Apply(context.Background(), Env{DataDir: dir}, Candidate{}, ApplyOpts{})
	if !errors.Is(err, platform.ErrWindowsAction) {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("refusal touched data directory", err)
	}
	if got := scratchCandidates(context.Background(), Env{TempRoots: []string{dir}}, snapshot{}); len(got) != 0 {
		t.Fatal(got)
	}
}
