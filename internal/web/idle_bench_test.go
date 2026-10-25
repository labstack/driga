package web

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/viewer"
)

type idleSource struct{}

func (idleSource) LoadScope(context.Context, store.Scope, ...bool) (viewer.Snapshot, error) {
	return viewer.Snapshot{}, nil
}
func (idleSource) DataVersion(context.Context) (int64, error) { return 1, nil }

// A page's first poll reads the cursor and the initial tail at once.
// Repeated first polls model page loads without network or timer jitter.
func BenchmarkDashboardFirstPoll(b *testing.B) {
	s, err := New(idleSource{}, Options{})
	if err != nil {
		b.Fatal(err)
	}
	close(s.ready)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/updates", nil))
	}
}
