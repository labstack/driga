package store

import "time"

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

// CloseAttentionBefore closes older waiting requests after an explicit user signal.
func (tx *Tx) CloseAttentionBefore(sessionID int64, at time.Time) error {
	// A worker's attention item can live on its parent. Its natural item ID
	// retains the worker's adapter/external ID, so close that request as well.
	return tx.exec(`UPDATE inbox_items SET acked_at=? WHERE machine_id=? AND
	(session_id=? OR substr(id,1,length((SELECT adapter||':'||external_id||':' FROM sessions WHERE id=? AND machine_id=?)))=
	(SELECT adapter||':'||external_id||':' FROM sessions WHERE id=? AND machine_id=?))
	AND kind='waiting' AND ts<? AND acked_at IS NULL`, unix(at), tx.machineID, sessionID, sessionID, tx.machineID, sessionID, tx.machineID, unix(at))
}
