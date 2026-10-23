package actions

import (
	"context"
	"errors"
	"github.com/labstack/driga/internal/model"
	"os"
	"path/filepath"
	"testing"
)

type failedOutput struct{}

var errOutput = errors.New("output failed")

func (failedOutput) Write([]byte) (int, error) { return 0, errOutput }
func TestJumpReportsOutputFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DRIGA_CONFIG", path)
	err := Jump(context.Background(), Environment{Stdout: failedOutput{}}, nil, &model.InboxJump{Kind: "resume", Command: "copyable resume command"})
	if !errors.Is(err, errOutput) {
		t.Fatalf("failed output reported success: %v", err)
	}
}
