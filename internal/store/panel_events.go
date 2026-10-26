package store

import (
	"encoding/json"
	"github.com/labstack/driga/internal/model"
	"strings"
	"time"
)

func panelEvent(kind string) bool {
	if strings.HasPrefix(kind, "log:") {
		return true
	}
	switch kind {
	case "UserPromptSubmit", "userPromptSubmitted", "otlp:user_prompt", "Stop", "agentStop", "errorOccurred", "SessionEnd", "sessionEnd", "PostToolUse", "PostToolUseFailure", "postToolUse", "postToolUseFailure", "otlp:tool_result":
		return true
	}
	return false
}

// RecordPanelEvent discards summary/input text even under snippets capture.
// The bool reports a new identity so adapters publish only new observations.
func (tx *Tx) RecordPanelEvent(e model.Event) (bool, error) {
	if !panelEvent(e.Kind) {
		return false, nil
	}
	// Only a protocol tool name is needed; token/cost metadata is not retained here.
	var metadata *model.EventMetadata
	if e.Metadata != nil && e.Metadata.Tool != "" {
		metadata = &model.EventMetadata{Tool: e.Metadata.Tool}
	}
	b, err := json.Marshal(metadata)
	if err != nil {
		return false, err
	}
	res, err := tx.conn.ExecContext(tx.ctx, "INSERT OR IGNORE INTO panel_events(machine_id,adapter,external_id,ts,kind,metadata) VALUES(?,?,?,?,?,?)", tx.machineID, e.Adapter, e.ExternalID, unix(e.TS), e.Kind, string(b))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// PanelEvents retains tool-call identities for a day and lifecycle evidence for
// seven days, independently of the raw event tail and capture_text.
func (q *querier) PanelEvents(adapter, external string, since time.Time) ([]model.Event, error) {
	return readMany(q, "SELECT machine_id,adapter,external_id,ts,kind,summary,metadata FROM panel_events WHERE machine_id=? AND adapter=? AND external_id=? AND ts>=? ORDER BY ts,kind", []any{q.machineID, adapter, external, unix(since)}, scanEvent)
}
