package web

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/viewer"
)

type countingSource struct {
	Source
	loads int
}

func (s *countingSource) LoadScope(ctx context.Context, scope store.Scope, all ...bool) (viewer.Snapshot, error) {
	s.loads++
	return s.Source.LoadScope(ctx, scope, all...)
}
func TestOverviewAndCleanupCaches(t *testing.T) {
	base, _, _, _ := fixture(t)
	source := &countingSource{Source: base.source}
	s, err := New(source, base.opts)
	if err != nil {
		t.Fatal(err)
	}
	cleanups := 0
	s.opts.CleanupList = func(context.Context) ([]cleanup.Candidate, error) { cleanups++; return nil, nil }
	for i := 0; i < 2; i++ {
		p, code, err := s.loadPage(httptest.NewRequest("GET", "/", nil))
		if err != nil || code != 200 {
			t.Fatal(code, err)
		}
		if len(p.AgentDetails) != 0 {
			t.Fatal("Home built agent details")
		}
	}
	if source.loads != 1 || cleanups != 1 {
		t.Fatalf("loads=%d cleanups=%d", source.loads, cleanups)
	}
}
