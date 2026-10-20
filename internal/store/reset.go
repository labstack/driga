package store

import "context"

// ResetMachineID re-stamps local rows, preserves remote rows and starts a new
// feed epoch. The caller must hold the collector's exclusive writer lock.
func (s *Store) ResetMachineID(ctx context.Context) error {
	id, err := newMachineID()
	if err != nil {
		return err
	}
	epoch, err := randomID()
	if err != nil {
		return err
	}
	err = s.Tx(ctx, func(tx *Tx) error {
		for _, table := range []string{"meta", "machines", "repos", "worktrees", "sessions", "session_files", "events", "event_minutes", "observation_minutes", "handoffs", "summaries", "conversations", "messages", "procs", "orphans", "machine_samples", "offsets", "inbox_items", "sources", "gaps"} {
			if err := tx.exec("UPDATE "+table+" SET machine_id=? WHERE machine_id=?", id, s.machineID); err != nil {
				return err
			}
		}
		if err := tx.exec("UPDATE machines SET id=?,device_id='',feed_epoch='',feed_seq=0 WHERE machine_id=?", id, id); err != nil {
			return err
		}
		if err := tx.exec("UPDATE meta SET value=? WHERE key='machine_id'", id); err != nil {
			return err
		}
		if err := tx.exec("UPDATE meta SET value=? WHERE key='feed_epoch'", epoch); err != nil {
			return err
		}
		return tx.exec("DELETE FROM feed")
	})
	if err == nil {
		s.machineID = id
		s.localID = id
	}
	return err
}
