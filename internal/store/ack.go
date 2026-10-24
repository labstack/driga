package store

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/labstack/driga/internal/model"
)

// MarkAckPending queues an existing remote item without acknowledging it locally.
// Repeated requests retain the first pending timestamp.
func (tx *Tx) MarkAckPending(id string, at time.Time) error {
	if tx.machineID == tx.localID {
		return fmt.Errorf("pending ack requires a remote machine")
	}
	return tx.exec("UPDATE inbox_items SET ack_pending_at=COALESCE(ack_pending_at,?) WHERE machine_id=? AND id=? AND acked_at IS NULL", unix(at), tx.machineID, id)
}

// PendingAcks returns durable requests awaiting authoritative peer receipts.
func (q *querier) PendingAcks(scope Scope) ([]model.InboxItem, error) {
	items, err := q.InboxScoped(time.Time{}, true, scope)
	if err != nil {
		return nil, err
	}
	var pending []model.InboxItem
	for _, item := range items {
		if item.AckPendingAt != nil && item.AckedAt == nil {
			pending = append(pending, item)
		}
	}
	return pending, nil
}

// EndMachineWarning retains warning history while preventing alerts on recovery.
func (tx *Tx) EndMachineWarning(kind string, at time.Time) error {
	if kind != "low_disk" && kind != "memory_pressure" {
		return fmt.Errorf("invalid machine warning %q", kind)
	}
	return tx.exec("UPDATE inbox_items SET ended_at=? WHERE machine_id=? AND session_id=0 AND kind=? AND ended_at IS NULL", unix(at), tx.machineID, kind)
}

// RefreshMachineWarning upgrades an existing episode's text and click target,
// preserving its identity, acknowledgement, and notification delivery history.
func (tx *Tx) RefreshMachineWarning(kind, summary string, jump *model.InboxJump) error {
	if kind != "low_disk" && kind != "memory_pressure" {
		return fmt.Errorf("invalid machine warning %q", kind)
	}
	b, err := json.Marshal(jump)
	if err != nil {
		return err
	}
	return tx.exec("UPDATE inbox_items SET summary=?,jump=? WHERE machine_id=? AND session_id=0 AND kind=? AND ended_at IS NULL AND acked_at IS NULL", summary, string(b), tx.machineID, kind)
}
