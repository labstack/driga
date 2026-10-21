package codex_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/labstack/driga/internal/adapter/codex"
	"github.com/labstack/driga/internal/model"
)

type countingOffsets struct {
	offsets
	reads int
}

func (s *countingOffsets) GetOffset(path string) (model.Offset, bool, error) {
	s.reads++
	return s.offsets.GetOffset(path)
}

func TestOldDiscoverySkipsOffsetLookup(t *testing.T) {
	env, path := setup(t, fixture(t))
	old := env.Now.Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	o := &countingOffsets{offsets: offsets{}}
	env.Offsets = o
	if _, _, err := codex.New().Collect(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if o.reads != 0 {
		t.Fatalf("old discovery queried offsets %d times", o.reads)
	}
}
