package fleet

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/labstack/driga/internal/model"
)

// Cursor identifies an atomic position in a peer's feed.
type Cursor struct {
	Epoch string `json:"epoch"`
	Seq   int64  `json:"seq"`
}

// Snapshot is the fleet state and its resume position from one read transaction.
type Snapshot struct {
	Schema       int                      `json:"schema"`
	ObservedAt   time.Time                `json:"observed_at"`
	Mode         string                   `json:"mode"`
	Freshness    map[string]SnapshotFresh `json:"freshness"`
	CoverageGaps []model.Gap              `json:"coverage_gaps"`
	Data         SnapshotData             `json:"data"`
}

// SnapshotFresh uses the same freshness shape as local view envelopes.
type SnapshotFresh struct {
	LastOKAt  time.Time `json:"last_ok_at"`
	AgeMS     int64     `json:"age_ms"`
	Stale     bool      `json:"stale"`
	LastError string    `json:"last_error"`
}

// SnapshotData carries privacy-filtered rows with natural references only.
type SnapshotData struct {
	Machine       json.RawMessage   `json:"machine"`
	Cursor        Cursor            `json:"cursor"`
	Sessions      []json.RawMessage `json:"sessions"`
	SessionFiles  []json.RawMessage `json:"session_files"`
	Events        []json.RawMessage `json:"events"`
	Handoffs      []json.RawMessage `json:"handoffs"`
	Summaries     []json.RawMessage `json:"summaries"`
	Repos         []json.RawMessage `json:"repos"`
	Worktrees     []json.RawMessage `json:"worktrees"`
	Procs         []json.RawMessage `json:"procs"`
	Orphans       []json.RawMessage `json:"orphans"`
	InboxItems    []json.RawMessage `json:"inbox_items"`
	MachineSample json.RawMessage   `json:"machine_sample"`
	Sources       []json.RawMessage `json:"sources"`
	Gaps          []json.RawMessage `json:"gaps"`
}

type snapshotFile struct {
	model.FileTouch
	SessionID int64 `json:"session_id"`
}
type snapshotHandoff struct {
	MachineID string    `json:"machine_id"`
	SessionID int64     `json:"session_id"`
	TS        time.Time `json:"occurred_at"`
	Text      string    `json:"text"`
}

// snapshotRows binds the existing model JSON fields to SQLite columns. Keeping
// this read-only projection here avoids exposing SQL or new writes in Store.
// All identifiers and suffixes come from the fixed queries above.
func snapshotRows[T any](ctx context.Context, tx *sql.Tx, table, machine, suffix string, visit func(T) error, args ...any) error {
	var sample T
	type binding struct {
		index  []int
		column string
		typ    reflect.Type
	}
	var fields []binding
	timeType := reflect.TypeFor[time.Time]()
	var walk func(reflect.Type, []int)
	walk = func(typ reflect.Type, path []int) {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			index := append(append([]int{}, path...), i)
			if f.Anonymous {
				walk(f.Type, index)
				continue
			}
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name == "" || name == "-" || name == "completions" || name == "files_touched" || name == "parent_external_id" {
				continue
			}
			// Wire names and historical SQL columns are separate contracts.
			columns := map[string]string{"mem_total_bytes": "mem_total", "mem_used_bytes": "mem_used", "mem_compressed_bytes": "mem_compressed", "swap_used_bytes": "swap_used", "swap_total_bytes": "swap_total", "disk_free_bytes": "disk_free", "cpu_ms": "cpu_seconds", "rss_bytes": "rss", "rss_total_bytes": "rss_total", "first_seen_at": "first_seen", "last_seen_at": "last_seen", "proc_started_at": "proc_start", "oldest_dirty_modified_at": "oldest_dirty_mtime", "occurred_at": "ts", "sampled_at": "ts"}
			if column, ok := columns[name]; ok {
				name = column
			}
			if table == "gaps" {
				if name == "started_at" {
					name = "from_ts"
				}
				if name == "ended_at" {
					name = "to_ts"
				}
			}
			if (table == "procs" || table == "orphans") && name == "started_at" {
				name = "start_time"
			}
			switch name {
			case "parent_id":
				name = "parent_session_id"
			case "label":
				name = "state"
			case "heuristic":
				name = "state_heuristic"
			case "from":
				name = "from_ts"
			case "to":
				name = "to_ts"
			}
			if table == "session_files" && name == "ts" {
				name = "last_edit_at"
			}
			fields = append(fields, binding{index, name, f.Type})
		}
	}
	walk(reflect.TypeOf(sample), nil)
	cols := make([]string, len(fields))
	for i, f := range fields {
		cols[i] = f.column
	}
	allArgs := append([]any{machine}, args...)
	rows, err := tx.QueryContext(ctx, "SELECT "+strings.Join(cols, ",")+" FROM "+table+" WHERE machine_id=?"+suffix, allArgs...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item T
		v := reflect.ValueOf(&item).Elem()
		dest := make([]any, len(fields))
		finish := []func() error{}
		for i, f := range fields {
			field := v.FieldByIndex(f.index)
			switch {
			case f.typ == timeType || f.typ == reflect.PointerTo(timeType):
				n := new(sql.NullInt64)
				dest[i] = n
				finish = append(finish, func() error {
					if !n.Valid {
						return nil
					}
					at := time.Time{}
					if n.Int64 != 0 {
						at = time.Unix(0, n.Int64).UTC()
					}
					if field.Kind() == reflect.Pointer {
						field.Set(reflect.ValueOf(&at))
					} else {
						field.Set(reflect.ValueOf(at))
					}
					return nil
				})
			case f.typ.Kind() == reflect.Slice:
				raw := new(string)
				dest[i] = raw
				finish = append(finish, func() error { return json.Unmarshal([]byte(*raw), field.Addr().Interface()) })
			default:
				dest[i] = field.Addr().Interface()
			}
		}
		if err := rows.Scan(dest...); err != nil {
			return err
		}
		for _, fn := range finish {
			if err := fn(); err != nil {
				return err
			}
		}
		if err := visit(item); err != nil {
			return err
		}
	}
	return rows.Err()
}
