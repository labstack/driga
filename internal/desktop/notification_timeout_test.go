package desktop

import (
	"context"
	"errors"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/viewer"
	"testing"
	"time"
)

type notificationSource struct{ t *testing.T }

func (s notificationSource) LoadScope(ctx context.Context, _ store.Scope, _ ...bool) (viewer.Snapshot, error) {
	at, ok := ctx.Deadline()
	if !ok || time.Until(at) > 5*time.Second {
		s.t.Fatal("notification load has no bounded deadline")
	}
	return viewer.Snapshot{}, context.DeadlineExceeded
}
func TestNotificationLookupHasDeadline(t *testing.T) {
	_, err := notificationSnapshot(context.Background(), notificationSource{t})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
