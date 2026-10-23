package derive

import (
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
)

// Classifier reuses signature matches within one process pass. It belongs to
// that pass, not to a PID or the collector's long-lived state.
type Classifier struct {
	sigs  *signatures.Set
	cache map[processKey]classification
}

type processKey struct {
	pid          int32
	start        time.Time
	cmdline, exe string
}

type classification struct {
	sig signatures.Signature
	ok  bool
}

func NewClassifier(sigs *signatures.Set, size int) *Classifier {
	return &Classifier{sigs: sigs, cache: make(map[processKey]classification, size)}
}

func (c *Classifier) Match(p model.Proc) (signatures.Signature, bool) {
	key := processKey{p.PID, p.StartTime, p.Cmdline, p.Exe}
	if result, ok := c.cache[key]; ok {
		return result.sig, result.ok
	}
	var result classification
	if c.sigs != nil {
		result.sig, result.ok = c.sigs.Match(p.Cmdline, p.Exe)
	}
	c.cache[key] = result
	return result.sig, result.ok
}
