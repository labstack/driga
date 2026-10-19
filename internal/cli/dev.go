package cli

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
	_ "modernc.org/sqlite"
)

// DevCmd contains hidden, on-demand format discovery commands.
type DevCmd struct {
	AgySchema DevAgySchemaCmd `cmd:"" help:"Inspect agy database schemas without printing values."`
}

// DevAgySchemaCmd reads only conversation database schema and row counts.
type DevAgySchemaCmd struct {
	Dir string `help:"Directory containing conversation DBs, or an antigravity-cli root." type:"path"`
}

type agyColumn struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	NotNull    bool   `json:"not_null"`
	PrimaryKey int    `json:"primary_key"`
}
type agyTable struct {
	Name    string      `json:"name"`
	Columns []agyColumn `json:"columns"`
	Rows    int64       `json:"rows"`
}
type agySchema struct {
	Hash      string     `json:"hash"`
	Databases int        `json:"databases"`
	Tables    []agyTable `json:"tables"`
}
type agySchemaReport struct {
	Schemas  []agySchema `json:"schemas"`
	Warnings []string    `json:"warnings"`
}

func openAgySchemaDB(path string) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: abs}
	q := url.Values{"mode": {"ro"}, "immutable": {"0"}, "_pragma": {"busy_timeout(100)", "query_only(1)"}}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func quoteAgyIdentifier(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

func inspectAgySchema(ctx context.Context, path string) ([]agyTable, error) {
	db, err := openAgySchemaDB(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	// One read transaction keeps schema and counts from different snapshots
	// from accidentally being grouped together during a concurrent migration.
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	tables := make([]agyTable, 0, len(names))
	for _, name := range names {
		table := agyTable{Name: name, Columns: []agyColumn{}}
		columns, err := tx.QueryContext(ctx, "PRAGMA table_info("+quoteAgyIdentifier(name)+")")
		if err != nil {
			return nil, err
		}
		for columns.Next() {
			var c agyColumn
			var cid, notnull int
			var unusedDefault sql.NullString
			if err := columns.Scan(&cid, &c.Name, &c.Type, &notnull, &unusedDefault, &c.PrimaryKey); err != nil {
				columns.Close()
				return nil, err
			}
			c.NotNull = notnull != 0
			table.Columns = append(table.Columns, c)
		}
		err = columns.Err()
		columns.Close()
		if err != nil {
			return nil, err
		}
		// Counts are the only table-data query; never select row values, SQL
		// definitions, defaults, brain or knowledge contents.
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+quoteAgyIdentifier(name)).Scan(&table.Rows); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}
	return tables, tx.Commit()
}

func (c DevAgySchemaCmd) Run(g *Globals) error {
	dir := c.Dir
	if dir == "" {
		dir = filepath.Join(config.ExpandHome("~"), ".gemini", "antigravity-cli", "conversations")
	}
	if info, err := os.Stat(filepath.Join(dir, "conversations")); err == nil && info.IsDir() {
		dir = filepath.Join(dir, "conversations")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("agy schema source: %w", err)
	}
	report := agySchemaReport{Schemas: []agySchema{}, Warnings: []string{}}
	groups := map[string]int{}
	for _, entry := range entries {
		// Only direct, regular .db files; no recursion into brain/knowledge
		// and no following database symlinks outside this directory.
		if entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			report.Warnings = append(report.Warnings, "source: cannot inspect "+entry.Name())
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		tables, err := inspectAgySchema(ctx, filepath.Join(dir, entry.Name()))
		cancel()
		if err != nil {
			report.Warnings = append(report.Warnings, "source: cannot inspect "+entry.Name())
			continue
		}
		shape := append([]agyTable(nil), tables...)
		for i := range shape {
			shape[i].Rows = 0
		}
		canonical, _ := json.Marshal(shape)
		digest := sha256.Sum256(canonical)
		hash := hex.EncodeToString(digest[:])
		if i, ok := groups[hash]; ok {
			report.Schemas[i].Databases++
			for j, table := range tables {
				report.Schemas[i].Tables[j].Rows += table.Rows
			}
		} else {
			groups[hash] = len(report.Schemas)
			report.Schemas = append(report.Schemas, agySchema{Hash: hash, Databases: 1, Tables: tables})
		}
	}
	sort.Slice(report.Schemas, func(i, j int) bool { return report.Schemas[i].Hash < report.Schemas[j].Hash })
	if g != nil && g.JSON {
		return json.NewEncoder(commandOutput(g)).Encode(view.Envelope{Schema: 2, ObservedAt: time.Now().UTC(), Mode: "snapshot", Freshness: map[string]view.Fresh{}, CoverageGaps: []model.Gap{}, Data: report})
	}
	for _, schema := range report.Schemas {
		if _, err := fmt.Fprintf(commandOutput(g), "schema %s: %d databases\n", schema.Hash, schema.Databases); err != nil {
			return err
		}
		for _, table := range schema.Tables {
			if _, err := fmt.Fprintf(commandOutput(g), "  %s rows=%d (total)\n", table.Name, table.Rows); err != nil {
				return err
			}
			for _, col := range table.Columns {
				if _, err := fmt.Fprintf(commandOutput(g), "    %s %s\n", col.Name, col.Type); err != nil {
					return err
				}
			}
		}
	}
	for _, warning := range report.Warnings {
		if _, err := fmt.Fprintln(commandOutput(g), warning); err != nil {
			return err
		}
	}
	return nil
}
