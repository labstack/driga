package codex

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/labstack/driga/internal/pathx"
	"os"
	"strings"

	"github.com/labstack/driga/internal/adapter"
	_ "modernc.org/sqlite"
)

func openCatalog(ctx context.Context, path string) (*sql.DB, []adapter.Warning) {
	unavailable := func(err error) (*sql.DB, []adapter.Warning) {
		return nil, []adapter.Warning{{Kind: "catalog_unavailable", Detail: fmt.Sprintf("%s: %v", path, err)}}
	}
	if _, err := os.Stat(path); err != nil {
		return unavailable(err)
	}
	db, err := sql.Open("sqlite", pathx.FileURL(path)+"?mode=ro&immutable=0")
	if err != nil {
		return unavailable(err)
	}
	db.SetMaxOpenConns(1)
	if err := checkCatalog(ctx, db); err != nil {
		db.Close()
		return unavailable(err)
	}
	return db, nil
}

func checkCatalog(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info(local_thread_catalog)")
	if err != nil {
		return err
	}
	defer rows.Close()
	var hasID, hasTitle bool
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, kind string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		hasID = hasID || strings.EqualFold(name, "id")
		hasTitle = hasTitle || strings.EqualFold(name, "display_title")
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !hasID || !hasTitle {
		return errors.New("local_thread_catalog requires id and display_title columns")
	}
	return nil
}

func catalogTitle(ctx context.Context, db *sql.DB, id string) (string, error) {
	var title sql.NullString
	err := db.QueryRowContext(ctx, "SELECT display_title FROM local_thread_catalog WHERE id = ?", id).Scan(&title)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return title.String, err
}
