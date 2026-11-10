package platform

import (
	"context"

	"github.com/labstack/driga/internal/model"
)

// ProcessObserver resolves a PID omitted by a resource snapshot. A successful
// present=false observation proves absence; a query failure must return an error.
// Present observations need only PID, exact StartTime and Zombie, not readable
// arguments, executable paths or resource counters. It is not signal authority.
type ProcessObserver interface {
	ObserveProcess(context.Context, int32) (proc model.Proc, present bool, err error)
}
