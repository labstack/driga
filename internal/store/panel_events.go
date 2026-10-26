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

// RetainPanelEvent uses the same bounds as Prune, so a full transcript fold
// cannot resurrect identities that retention already removed.
func RetainPanelEvent(e model.Event, now time.Time) bool {
	cutoff := now.Add(-7 * 24 * time.Hour)
	if strings.HasPrefix(e.Kind, "log:tool_call:") || e.Kind == "PostToolUse" || e.Kind == "PostToolUseFailure" || e.Kind == "postToolUse" || e.Kind == "postToolUseFailure" || e.Kind == "otlp:tool_result" {
		cutoff = now.UTC().Truncate(time.Hour).Add(-23 * time.Hour)
	}
	return !e.TS.Before(cutoff)
}

// AllPanelEvents batches a machine's retained evidence for fleet panels.
func (q *querier) AllPanelEvents(since time.Time) ([]model.Event, error) {
	return readMany(q, "SELECT machine_id,adapter,external_id,ts,kind,summary,metadata FROM panel_events WHERE machine_id=? AND ts>=? ORDER BY ts,kind", []any{q.machineID, unix(since)}, scanEvent)
}

// RemoveInternalChat removes only a CLI identity explicitly registered by our
// Chat broker. Titles and directories never identify a private Chat process.
func (tx *Tx) RemoveInternalChat(external string) error {
	for _, table := range []string{"inbox_items", "handoffs"} {
		if err := tx.exec("DELETE FROM "+table+" WHERE machine_id=? AND session_id IN (SELECT id FROM sessions WHERE machine_id=? AND adapter='claude' AND external_id=?)", tx.machineID, tx.machineID, external); err != nil {
			return err
		}
	}
	for _, table := range []string{"events", "panel_events", "sessions"} {
		if err := tx.exec("DELETE FROM "+table+" WHERE machine_id=? AND adapter='claude' AND external_id=?", tx.machineID, external); err != nil {
			return err
		}
	}
	return nil
}
