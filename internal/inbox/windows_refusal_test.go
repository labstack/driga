package inbox

import (
	"context"
	"errors"
	"testing"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
)

func TestWindowsActionsRefuseBeforeCommands(t *testing.T) {
	r := runnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("Windows action launched a command")
		return nil, nil
	})
	_, err := RunJump(context.Background(), r, &model.InboxJump{Kind: "tmux", Target: "dev:1.2"}, JumpOptions{OS: "windows"})
	if !errors.Is(err, platform.ErrWindowsAction) {
		t.Fatal(err)
	}
	err = (DesktopNotifier{OS: "windows", Runner: r}).Notify(context.Background(), Notification{})
	if !errors.Is(err, platform.ErrWindowsAction) {
		t.Fatal(err)
	}
	var exit interface{ ExitCode() int }
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		t.Fatal(err)
	}
}
