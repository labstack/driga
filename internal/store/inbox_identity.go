package store

import "time"

// InboxIdentity is the active item's association needed to refresh destinations.
// No user text or serialized jump destination is loaded by this projection.
type InboxIdentity struct {
	ID        string
	SessionID int64
}

// ActiveInboxIdentities preserves Inbox(time.Time{}, false)'s machine and
// lifecycle filters without loading payloads that destination refresh discards.
func (q *querier) ActiveInboxIdentities() ([]InboxIdentity, error) {
	return readMany(q, "SELECT id,session_id FROM inbox_items WHERE machine_id=? AND ts>=? AND acked_at IS NULL AND ended_at IS NULL ORDER BY id", []any{q.machineID, unix(time.Time{})}, func(s scanner) (InboxIdentity, error) {
		var i InboxIdentity
		err := s.Scan(&i.ID, &i.SessionID)
		return i, err
	})
}
