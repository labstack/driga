// Package store persists collector state in SQLite. Each Store owns one connection.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/labstack/driga/internal/pathx"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // Register the pure-Go sqlite driver.
)

// Mode selects disk read/write, disk read-only, or isolated in-memory storage.
type Mode int

const (
	ReadWrite Mode = iota // ReadWrite creates and migrates a disk database.
	ReadOnly              // ReadOnly opens an existing database without migrating it.
	Memory                // Memory creates a private, migrated snapshot database.
)

// ErrNotFound is returned when a requested single row does not exist.
var ErrNotFound = sql.ErrNoRows

//go:embed migrations/*.sql
var migrations embed.FS

type queryDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// querier implements reads once for both the connection and its transactions.
type querier struct {
	conn      queryDB
	ctx       context.Context
	machineID string
	localID   string
	dataDir   string
}

// Store owns a single connection so PRAGMA data_version always observes the same
// connection. Inside Tx callbacks, use tx's read methods: calling s's methods
// there would wait for the connection held by the transaction and deadlock.
type Store struct {
	querier
	db *sql.DB
}

// Tx provides writes and the same reads as Store, on the active transaction.
// It must not escape the callback passed to Store.Tx.
type Tx struct{ querier }

// MachineTx stamps writes with one machine's identity in the parent transaction.
// It must not escape the Store.Tx callback.
type MachineTx struct{ *Tx }

// ForMachine returns a writer for id without changing the default local writer.
func (tx *Tx) ForMachine(id string) *MachineTx {
	q := tx.querier
	q.machineID = id
	return &MachineTx{Tx: &Tx{querier: q}}
}

// Open opens a database and migrates writable stores. Disk databases and their
// parent directories are restricted to the owner before SQLite opens the file.
func Open(path string, mode Mode) (_ *Store, retErr error) {
	var dsn string
	var dataDir string
	switch mode {
	case ReadWrite, ReadOnly:
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		dataDir = filepath.Dir(abs)
		if mode == ReadWrite {
			dir := filepath.Dir(abs)
			if err := os.MkdirAll(dir, 0700); err != nil {
				return nil, err
			}
			if err := os.Chmod(dir, 0700); err != nil {
				return nil, err
			}
			f, err := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0600)
			if err != nil {
				return nil, err
			}
			chmodErr := f.Chmod(0600)
			if err := errors.Join(chmodErr, f.Close()); err != nil {
				return nil, err
			}
		}
		dsn = pathx.FileURL(abs)
		if mode == ReadOnly {
			dsn += "?mode=ro&_pragma=busy_timeout(5000)"
		} else {
			dsn += "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
		}
	case Memory:
		id, err := randomID()
		if err != nil {
			return nil, err
		}
		dsn = "file:driga-snap-" + id + "?mode=memory&cache=shared"
	default:
		return nil, fmt.Errorf("invalid store mode %d", mode)
	}
	// Enable cascades on every connection, including any replacement connection.
	dsn += "&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	defer func() {
		if retErr != nil {
			_ = db.Close()
		}
	}()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	if mode != ReadOnly {
		subFS, err := fs.Sub(migrations, "migrations")
		if err != nil {
			return nil, err
		}
		provider, err := goose.NewProvider(goose.DialectSQLite3, db, subFS, goose.WithDisableGlobalRegistry(true), goose.WithGoMigrations(canonMigration()))
		if err != nil {
			return nil, err
		}
		// SQLite table rebuilds require foreign keys disabled outside the
		// migration transaction. This private single connection is not exposed
		// until migration completes and constraints have been restored.
		if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
			return nil, err
		}
		_, migrateErr := provider.Up(ctx)
		_, fkErr := db.ExecContext(ctx, "PRAGMA foreign_keys=ON")
		if err := errors.Join(migrateErr, fkErr); err != nil {
			return nil, fmt.Errorf("migrate: %w", err)
		}
		id, err := newMachineID()
		if err != nil {
			return nil, err
		}
		if _, err := db.ExecContext(ctx, "INSERT OR IGNORE INTO meta (machine_id, key, value) VALUES (?, 'machine_id', ?)", id, id); err != nil {
			return nil, err
		}
		epoch, err := randomID()
		if err != nil {
			return nil, err
		}
		if _, err := db.ExecContext(ctx, "INSERT OR IGNORE INTO meta (machine_id,key,value) SELECT machine_id,'feed_epoch',? FROM meta WHERE key='machine_id'", epoch); err != nil {
			return nil, err
		}
	}
	var id string
	if err := db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = 'machine_id'").Scan(&id); err != nil {
		return nil, fmt.Errorf("machine identity: %w", err)
	}
	return &Store{querier: querier{conn: db, ctx: ctx, machineID: id, localID: id, dataDir: dataDir}, db: db}, nil
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func newMachineID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// MachineID returns the persistent identity stamped on writes by this store.
func (q *querier) MachineID() string { return q.machineID }

// DataDir returns the database directory, or empty for an in-memory store.
func (q *querier) DataDir() string { return q.dataDir }

// Close releases the database connection.
func (s *Store) Close() error { return s.db.Close() }

// Tx commits fn's writes if fn succeeds, otherwise rolls them back (also on panic).
// Reads inside fn must use tx, rather than s, because the store has one connection.
func (s *Store) Tx(ctx context.Context, fn func(*Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(&Tx{querier{conn: tx, ctx: ctx, machineID: s.machineID, localID: s.localID, dataDir: s.dataDir}}); err != nil {
		return err
	}
	return tx.Commit()
}

// DataVersion changes when another connection commits a write. Compare values
// only on the same Store; a connection's own commits do not change its value.
func (q *querier) DataVersion(ctx context.Context) (int64, error) {
	var v int64
	err := q.conn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&v)
	return v, err
}

func unix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

func fromUnix(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return unix(*t)
}

func timePointer(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := fromUnix(n.Int64)
	return &t
}

func (q *querier) exec(query string, args ...any) error {
	_, err := q.conn.ExecContext(q.ctx, query, args...)
	return err
}
