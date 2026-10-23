// Package actions executes shared session, orphan, cleanup, acknowledgment and jump actions.
// Callers supply authorization; process identity and idle checks always run here.
package actions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

var ErrRefused = errors.New("not applied")

type Environment struct {
	Stdout         io.Writer
	Machine        string
	Platform       func() platform.Platform
	VerifyWindow   time.Duration
	Confirm        func(string) error
	OpenReader     func(context.Context) (*store.Store, string, func(), error)
	ResolveMachine func(view.Reader) (store.Scope, error)
}
type UsageError struct{ error }

func (UsageError) ExitCode() int                    { return 2 }
func InvalidUsage(format string, args ...any) error { return UsageError{fmt.Errorf(format, args...)} }
func Selection(id string, all bool) error {
	if (id == "") == !all {
		return InvalidUsage("provide an id or --all")
	}
	return nil
}
