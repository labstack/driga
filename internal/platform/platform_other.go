//go:build !darwin && !linux && !windows

package platform

import (
	"context"
	"time"

	"github.com/labstack/driga/internal/model"
)

type unsupportedPlatform struct{}

// New returns a collector that reports ErrUnsupported on this operating system.
func New() Platform { return unsupportedPlatform{} }

func (unsupportedPlatform) Procs(context.Context) ([]model.Proc, error) {
	return nil, ErrUnsupported
}

func (unsupportedPlatform) Machine(context.Context) (model.MachineSample, error) {
	return model.MachineSample{}, ErrUnsupported
}

func (unsupportedPlatform) Cwds(context.Context, []int32) (map[int32]string, error) {
	return nil, ErrUnsupported
}

func (unsupportedPlatform) Alive(int32, time.Time) bool { return false }
