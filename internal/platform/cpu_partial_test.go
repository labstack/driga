package platform

import (
	"context"
	"errors"
	"github.com/labstack/driga/internal/model"
	"testing"
)

func TestCPUCounterFailureKeepsHealthSample(t *testing.T) {
	s := model.MachineSample{MemTotal: 100, DiskTotal: 200, CPUTotalSeconds: 10}
	err := sampleCPU(context.Background(), &s, func(context.Context, *model.MachineSample) error { return errors.New("unavailable") })
	if err != nil || s.CPUError == "" || s.CPUTotalSeconds != 0 || s.CPUPercent != nil || s.MemTotal != 100 || s.DiskTotal != 200 {
		t.Fatalf("%+v %v", s, err)
	}
}
