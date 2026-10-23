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
		have, want, err := checkSchema(ctx, db, path)
		if err != nil {
			return nil, err
		}
		if have < want {
			// Only a writer applies forward migrations; a reader of an older
			// database is looking at a collector that has not restarted yet.
			if mode == ReadOnly {
				return nil, &SchemaError{Path: path, Behind: true}
			}
			provider, err := migrationProvider(db)
			if err != nil {
				return nil, err
			}
			if _, err := provider.Up(ctx); err != nil {
				return nil, fmt.Errorf("migrate schema: %w", err)
			}
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

// ErrSchemaBehind means a reader found a database its collector has not migrated yet.
var ErrSchemaBehind = errors.New("database schema is older than this driga")

// SchemaError carries the same diagnosis and fix for writers, readers and doctor.
type SchemaError struct {
	Path          string
	Newer, Behind bool
}

func (e *SchemaError) Unwrap() error {
	switch {
	case e.Newer:
		return ErrSchemaNewer
	case e.Behind:
		return ErrSchemaBehind
	}
	return ErrSchemaPreRelease
}

func (e *SchemaError) Fix() string {
	switch {
	case e.Newer:
		return "upgrade driga to the version that created this database"
	case e.Behind:
		return "restart the collector so it applies the update: driga service restart"
	}
	return resetCommand(e.Path, runtime.GOOS)
}

func (e *SchemaError) Error() string {
	switch {
	case e.Newer:
		return fmt.Sprintf("database at %s is from a newer driga; %s", e.Path, e.Fix())
	case e.Behind:
		return fmt.Sprintf("database at %s is older than this driga; %s", e.Path, e.Fix())
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

// checkSchema accepts the baseline and its forward migrations, returning the
// applied and the latest known version. It never creates or converts a history
// table, including on read-only opens; any other history requires a reset.
func checkSchema(ctx context.Context, db *sql.DB, path string) (have, want int64, err error) {
	incompatible := &SchemaError{Path: path}
	provider, err := migrationProvider(db)
	if err != nil {
		return 0, 0, err
	}
	known := map[int64]bool{}
	for _, source := range provider.ListSources() {
		known[source.Version] = true
		want = max(want, source.Version)
	}
	var marker int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&marker); err != nil {
		return 0, 0, err
	}
	var exists int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='goose_db_version'").Scan(&exists); err != nil {
		return 0, 0, err
	}
	if exists != 1 || marker != 1 {
		return 0, 0, incompatible
	}
	rows, err := db.QueryContext(ctx, "SELECT version_id,is_applied FROM goose_db_version ORDER BY id")
	if err != nil {
		return 0, 0, incompatible
	}
	defer rows.Close()
	var versions []int64
	for rows.Next() {
		var version int64
		var applied bool
		if err := rows.Scan(&version, &applied); err != nil || !applied {
			return 0, 0, incompatible
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	if len(versions) < 2 || versions[0] != 0 || versions[1] != 1 {
		return 0, 0, incompatible
	}
	// The baseline's descendants form a strictly increasing history.
	have = 1
	for _, v := range versions[2:] {
		if v <= have {
			return 0, 0, incompatible
		}
		have = v
	}
	if have > want {
		return 0, 0, &SchemaError{Path: path, Newer: true}
	}
	for _, v := range versions[1:] {
		if !known[v] {
			return 0, 0, incompatible
		}
	}
	return have, want, nil
}
