package derive

import (
	"time"

	"github.com/labstack/driga/internal/platform"
)

// SameStart compares a kernel create time with a recorded process start.
func SameStart(create, recorded time.Time) bool {
	return platform.SameStart(create, recorded)
}
