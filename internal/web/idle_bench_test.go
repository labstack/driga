package web

import (
	"context"
	"net/http"
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

type idleWriter struct {
	header http.Header
	cancel context.CancelFunc
}

func (w *idleWriter) Header() http.Header       { return w.header }
func (*idleWriter) WriteHeader(int)             {}
func (*idleWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *idleWriter) Flush()                    { w.cancel() }

// A disconnect after ready still runs the initial tail and scheduler setup.
// Repeated connects model browser reconnects without network or timer jitter.
func BenchmarkDashboardConnect(b *testing.B) {
	s, err := New(idleSource{}, Options{Host: "127.0.0.1:4321"})
	if err != nil {
		b.Fatal(err)
	}
	close(s.ready)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ctx, cancel := context.WithCancel(context.Background())
		r := httptest.NewRequest("GET", "/events", nil).WithContext(ctx)
		s.events(&idleWriter{header: http.Header{}, cancel: cancel}, r)
		cancel()
	}
}
