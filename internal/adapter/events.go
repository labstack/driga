package adapter

import (
	"crypto/sha256"
	"fmt"
	"github.com/labstack/driga/internal/model"
	"time"
)

// ToolEvent retains protocol metadata only. Hash the protocol identity so the
// durable deduplication key cannot accidentally retain argument text.
func ToolEvent(at time.Time, tool, identity string) model.Event {
	return model.Event{TS: at, Kind: fmt.Sprintf("log:tool_call:%x", sha256.Sum256([]byte(identity))), Metadata: &model.EventMetadata{Tool: tool}}
}
