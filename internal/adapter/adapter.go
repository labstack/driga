// Package adapter defines the inputs and observations shared by agent adapters.
package adapter

import (
	"context"
	"time"

	"github.com/labstack/driga/internal/model"
)

// Env supplies filesystem, clock, privacy and process evidence for a collection.
type Env struct {
	Home            string
	Now             time.Time
	CaptureSnippets bool
	Offsets         OffsetStore
	Alive           func(pid int32, start time.Time) bool
	Baseline        bool
}

// OffsetStore persists tail positions keyed by absolute transcript path.
type OffsetStore interface {
	GetOffset(path string) (model.Offset, bool, error)
	SetOffset(path string, o model.Offset) error
}

// Adapter collects metadata and new transcript observations for one agent.
type Adapter interface {
	Name() string
	Collect(ctx context.Context, env Env) ([]model.SessionObs, []Warning, error)
	TestedVersions() []string
}

// Warning reports a recoverable source or format issue.
type Warning struct{ Kind, Detail string }
