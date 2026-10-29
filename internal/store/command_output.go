package store

import (
	"context"
	"errors"
	"io"
)

// AppendCommandOutput accepts capture-filtered chunks for a local action only.
func (s *Store) AppendCommandOutput(ctx context.Context, actionID, stream string, seq int, data []byte) error {
	result, err := s.db.ExecContext(ctx, `INSERT INTO chat_command_output(action_id,stream,seq,data) SELECT id,?,?,? FROM messages WHERE id=? AND machine_id=? AND role='action'`, stream, seq, data, actionID, s.MachineID())
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return errors.New("local action not found")
	}
	return err
}
func (s *Store) ReadCommandOutput(ctx context.Context, actionID string, dst io.Writer) error {
	rows, err := s.db.QueryContext(ctx, `SELECT o.data FROM chat_command_output o JOIN messages m ON m.id=o.action_id WHERE o.action_id=? AND m.machine_id=? ORDER BY o.seq`, actionID, s.MachineID())
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var data []byte
		if err = rows.Scan(&data); err != nil {
			return err
		}
		n, e := dst.Write(data)
		if e != nil {
			return e
		}
		if n != len(data) {
			return io.ErrShortWrite
		}
	}
	return rows.Err()
}
