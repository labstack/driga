package kill

import (
	"context"
	"errors"
	"testing"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
)

func TestWindowsKillRefusedBeforeInspection(t *testing.T) {
	_, err := Tree(context.Background(), nil, model.Orphan{}, nil, false, 0)
	if !errors.Is(err, platform.ErrWindowsAction) {
		t.Fatal(err)
	}
}
