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
	"github.com/labstack/driga/internal/secfile"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // Register the pure-Go sqlite driver.
)

// Mode selects disk read/write, disk read-only, or isolated in-memory storage.
type Mode int

const (
	ReadWrite Mode = iota // ReadWrite creates or opens a baseline disk database.
	ReadOnly              // ReadOnly opens an existing baseline database.
	Memory                // Memory creates a private snapshot database with the baseline schema.
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

// Open opens a baseline database, creating its schema only when empty. Files and their
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
		path = abs
		dataDir = filepath.Dir(abs)
		if mode == ReadWrite {
			dir := filepath.Dir(abs)
			if err := secfile.MkdirPrivate(dir); err != nil {
				return nil, err
			}
			if err := secfile.Protect(dir); err != nil {
				return nil, err
			}
			f, err := secfile.OpenFile(abs, os.O_CREATE|os.O_RDWR)
			if err != nil {
				return nil, err
			}
			chmodErr := secfile.ProtectFile(f)
			if err := errors.Join(chmodErr, f.Close()); err != nil {
				return nil, err
			}
		}
		dsn = pathx.FileURL(abs)
		if mode == ReadOnly {
			dsn += "?mode=ro&_pragma=busy_timeout(5000)"
		} else {
			dsn += "?_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
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
	var objects int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").Scan(&objects); err != nil {
		return nil, err
	}
	fresh := objects == 0 && mode != ReadOnly
	if !fresh {
		if err := checkSchema(ctx, db, path); err != nil {
			return nil, err
		}
	}
	if fresh {
		// Set the page layout before goose creates even its history table.
		if _, err := db.ExecContext(ctx, "PRAGMA auto_vacuum=INCREMENTAL"); err != nil {
			return nil, err
		}
		provider, err := migrationProvider(db)
		if err != nil {
			return nil, err
		}
		if _, err := provider.Up(ctx); err != nil {
			return nil, fmt.Errorf("create schema: %w", err)
		}
	}
	if mode != ReadOnly {
		if mode == ReadWrite {
			if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
				return nil, err
			}
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
	if err := ValidateMachineID(id); err != nil {
		return nil, err
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

// ValidateMachineID requires a canonical UUIDv7 machine identity.
func ValidateMachineID(id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 7 || parsed.Variant() != uuid.RFC4122 || parsed.String() != id {
		return fmt.Errorf("machine_id must be a canonical UUIDv7")
	}
	return nil
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

// ErrSchemaPreRelease means the database must be reset, with no conversion.
var ErrSchemaPreRelease = errors.New("database uses a pre-release schema")

// ErrSchemaNewer means the database belongs to a newer driga binary.
var ErrSchemaNewer = errors.New("database is from a newer driga")

// SchemaError carries the same diagnosis and fix for writers, readers and doctor.
type SchemaError struct {
	Path  string
	Newer bool
}

func (e *SchemaError) Unwrap() error {
	if e.Newer {
		return ErrSchemaNewer
	}
	return ErrSchemaPreRelease
}

func (e *SchemaError) Fix() string {
	if e.Newer {
		return "upgrade driga to the version that created this database"
	}
	return resetCommand(e.Path, runtime.GOOS)
}

func (e *SchemaError) Error() string {
	if e.Newer {
		return fmt.Sprintf("database at %s is from a newer driga; %s", e.Path, e.Fix())
	}
	return fmt.Sprintf("database at %s uses a pre-release schema; reset it: %s", e.Path, e.Fix())
}

func resetCommand(path, goos string) string {
	paths := []string{path, path + "-wal", path + "-shm"}
	for i, p := range paths {
		if goos == "windows" {
			paths[i] = "'" + strings.ReplaceAll(p, "'", "''") + "'"
		} else {
			paths[i] = "'" + strings.ReplaceAll(p, "'", "'\"'\"'") + "'"
		}
	}
	if goos == "windows" {
		return "driga service stop; if ($LASTEXITCODE -eq 0) { Remove-Item -LiteralPath " + strings.Join(paths, ", ") + " -Force -ErrorAction SilentlyContinue; driga service start }"
	}
	return "driga service stop && rm -f -- " + strings.Join(paths, " ") + " && driga service start"
}

func migrationProvider(db *sql.DB) (*goose.Provider, error) {
	subFS, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectSQLite3, db, subFS, goose.WithDisableGlobalRegistry(true))
}

// checkSchema accepts only the baseline's history. It never creates or converts
// a history table, including on read-only opens.
func checkSchema(ctx context.Context, db *sql.DB, path string) error {
	incompatible := &SchemaError{Path: path}
	var marker int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&marker); err != nil {
		return err
	}
	var exists int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='goose_db_version'").Scan(&exists); err != nil {
		return err
	}
	if exists != 1 {
		return incompatible
	}
	rows, err := db.QueryContext(ctx, "SELECT version_id,is_applied FROM goose_db_version ORDER BY id")
	if err != nil {
		return incompatible
	}
	defer rows.Close()
	var versions []int64
	for rows.Next() {
		var version int64
		var applied bool
		if err := rows.Scan(&version, &applied); err != nil {
			return incompatible
		}
		if !applied {
			return incompatible
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if marker < 1 || len(versions) < 2 || versions[0] != 0 || versions[1] != 1 {
		return incompatible
	}
	// A newer baseline descendant has a strictly increasing history. Legacy and
	// unknown chains lack the baseline marker and always require a reset.
	previous := int64(1)
	for _, v := range versions[2:] {
		if v <= previous {
			return incompatible
		}
		previous = v
	}
	if marker > 1 || previous > 1 {
		return &SchemaError{Path: path, Newer: true}
	}
	if marker != 1 {
		return incompatible
	}
	return nil
}
