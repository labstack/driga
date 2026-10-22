package store

import (
	"context"
	"database/sql"
)

// StorageBudgetBytes is the doctor warning threshold for the database plus WAL.
const StorageBudgetBytes int64 = 500_000_000

type StorageUsage struct {
	Bytes             int64
	LargestTable      string
	LargestTableBytes int64
}

// StorageUsage includes indexes in their owning table's size, using SQLite's
// page map rather than reading or decoding user records. It is read-only.
func (q *querier) StorageUsage() (StorageUsage, error) {
	var u StorageUsage
	var pages, pageSize int64
	if err := q.conn.QueryRowContext(q.ctx, "PRAGMA page_count").Scan(&pages); err != nil {
		return u, err
	}
	if err := q.conn.QueryRowContext(q.ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return u, err
	}
	u.Bytes = pages * pageSize
	err := q.conn.QueryRowContext(q.ctx, `SELECT COALESCE(s.tbl_name,d.name),SUM(d.pgsize) FROM dbstat d LEFT JOIN sqlite_schema s ON s.name=d.name GROUP BY COALESCE(s.tbl_name,d.name) ORDER BY SUM(d.pgsize) DESC LIMIT 1`).Scan(&u.LargestTable, &u.LargestTableBytes)
	return u, err
}

// ReclaimSpace returns at most 256 free pages per maintenance pass. Run after
// pruning commits; keeping the batch small bounds writer work and WAL growth.
// Readers retain their WAL snapshots throughout this operation.
func (s *Store) ReclaimSpace(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "PRAGMA incremental_vacuum(256)")
	return err
}

// enableIncrementalVacuum runs the expensive rebuild only for a database whose
// durable page layout is not yet incremental. Read-only opens never call it.
func enableIncrementalVacuum(ctx context.Context, db *sql.DB) error {
	var mode int
	if err := db.QueryRowContext(ctx, "PRAGMA auto_vacuum").Scan(&mode); err != nil {
		return err
	}
	if mode == 2 {
		return nil
	}
	if _, err := db.ExecContext(ctx, "PRAGMA auto_vacuum=INCREMENTAL"); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, "VACUUM")
	return err
}
