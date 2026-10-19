package store

import (
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
	items, err := q.InboxScoped(time.Time{}, false, scope)
	if err != nil {
		return nil, err
	}
	var pending []model.InboxItem
	for _, item := range items {
		if item.AckPendingAt != nil {
			pending = append(pending, item)
		}
	}
	return pending, nil
}
