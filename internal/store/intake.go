package store

import (
	"encoding/json"
	"time"
)

// IntakeState stores optional OTLP evidence separately from adapter observations.
type IntakeState struct {
	LastDecision, LastAPIRequest time.Time
	LastAPIError                 string
}

// IntakeState returns the intake evidence for an existing local session.
func (q *querier) IntakeState(id int64) (IntakeState, error) {
	var s IntakeState
	var decision, request int64
	err := q.conn.QueryRowContext(q.ctx, "SELECT last_decision_at,last_api_request_at,last_api_error FROM sessions WHERE machine_id=? AND id=?", q.machineID, id).Scan(&decision, &request, &s.LastAPIError)
	s.LastDecision, s.LastAPIRequest = fromUnix(decision), fromUnix(request)
	return s, err
}

// SetIntakeState annotates an existing session without changing its identity.
func (tx *Tx) SetIntakeState(id int64, s IntakeState) error {
	return tx.exec("UPDATE sessions SET last_decision_at=?,last_api_request_at=?,last_api_error=? WHERE machine_id=? AND id=?", unix(s.LastDecision), unix(s.LastAPIRequest), s.LastAPIError, tx.machineID, id)
}

// CloseAttentionBefore closes older waits after an explicit user or completion signal.
func (tx *Tx) CloseAttentionBefore(sessionID int64, at time.Time) error {
	return tx.exec(`UPDATE inbox_items SET ended_at=? WHERE machine_id=? AND session_id=?
	AND kind='waiting' AND waiting_since_at<? AND acked_at IS NULL AND ended_at IS NULL`, unix(at), tx.machineID, sessionID, unix(at))
}

// CloseAttentionActivity ends only waits that require a person's answer to progress.
func (tx *Tx) CloseAttentionActivity(sessionID int64, at time.Time) error {
	return tx.exec(`UPDATE inbox_items SET ended_at=? WHERE machine_id=? AND session_id=?
	AND kind='waiting' AND reason IN ('permission','question','elicitation')
	AND waiting_since_at<? AND acked_at IS NULL AND ended_at IS NULL`, unix(at), tx.machineID, sessionID, unix(at))
}

// CloseAttentionGrowth also covers a late hook drained in the same pass that
// measured growth. CPU counters and another session's log never close a wait.
func (tx *Tx) CloseAttentionGrowth(at time.Time) error {
	return tx.exec(`UPDATE inbox_items SET ended_at=(
 SELECT MIN(o.last_growth_at)*1000000 FROM observation_minutes o
 WHERE o.machine_id=inbox_items.machine_id AND o.session_id=inbox_items.session_id
 AND o.last_growth_at*1000000>inbox_items.waiting_since_at AND o.last_growth_at<=?)
 WHERE machine_id=? AND kind='waiting' AND reason IN ('permission','question','elicitation')
 AND acked_at IS NULL AND ended_at IS NULL AND EXISTS(
 SELECT 1 FROM observation_minutes o WHERE o.machine_id=inbox_items.machine_id
 AND o.session_id=inbox_items.session_id AND o.last_growth_at*1000000>inbox_items.waiting_since_at
 AND o.last_growth_at<=?)`, at.UnixMilli(), tx.machineID, at.UnixMilli())
}

func (tx *Tx) SetInboxJump(id string, jump any) error {
	b, err := json.Marshal(jump)
	if err != nil {
		return err
	}
	return tx.exec("UPDATE inbox_items SET jump=? WHERE machine_id=? AND id=?", string(b), tx.machineID, id)
}
