package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/pressly/goose/v3"
	"strconv"
	"strings"
	"time"
)

func canonMigration() *goose.Migration {
	return goose.NewGoMigration(8, &goose.GoFunc{RunTx: migrateCanon}, &goose.GoFunc{RunTx: func(context.Context, *sql.Tx) error {
		return fmt.Errorf("schema 2 contract migration cannot be downgraded")
	}})
}

var canonEnums = map[string]string{
	"same-worktree": "same_worktree", "same-file": "same_file", "same-file-other-worktree": "same_file_other_worktree",
	"worker-finished": "worker_finished", "low-disk": "low_disk", "needs-review": "needs_review", "report-only": "report_only",
	"not-yet-receiving": "not_yet_receiving", "last-event": "last_event", "bad-registry": "bad_registry", "unsupported-version": "unsupported_version", "untested-version": "untested_version",
	"invalid-json": "invalid_json", "oversize-line": "oversize_line", "skipped-backlog": "skipped_backlog",
	"catalog-unavailable": "catalog_unavailable", "reset-pending": "reset_pending", "enroll-close": "enroll_close", "enroll-decision": "enroll_decision", "enroll-poll": "enroll_poll",
	"collector-rss": "collector_rss", "data-permissions": "data_permissions",
}
var canonFields = map[string]string{
	"age": "age_ms", "last_seen_age": "last_seen_age_ms", "size": "size_bytes", "pos": "position_bytes", "rss": "rss_bytes", "rss_total": "rss_total_bytes",
	"mem": "mem_bytes", "mem_used": "mem_used_bytes", "mem_total": "mem_total_bytes", "mem_compressed": "mem_compressed_bytes",
	"swap_used": "swap_used_bytes", "swap_total": "swap_total_bytes", "disk_free": "disk_free_bytes", "reclaim": "reclaim_bytes",
	"first_seen": "first_seen_at", "last_seen": "last_seen_at", "start_time": "started_at", "proc_start": "proc_started_at",
	"oldest_dirty_mtime": "oldest_dirty_modified_at", "mtime": "modified_at", "from": "started_at", "to": "ended_at", "at": "occurred_at", "cpu_seconds": "cpu_ms", "age_seconds": "age_ms",
}

func migrateCanon(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET state_heuristic=1 WHERE state LIKE '~%';
 UPDATE sessions SET state=CASE ltrim(state,'~') WHEN 'stalled?' THEN 'stalled' ELSE ltrim(state,'~') END;`); err != nil {
		return err
	}
	for old, next := range canonEnums {
		for _, target := range []struct{ table, column string }{{"sessions", "state"}, {"machines", "state"}, {"events", "kind"}, {"inbox_items", "kind"}, {"feed", "kind"}} {
			if _, err := tx.ExecContext(ctx, "UPDATE "+target.table+" SET "+target.column+"=? WHERE "+target.column+"=?", next, old); err != nil {
				return err
			}
		}
		// Metadata warning keys and cleanup episode keys are owned names, not IDs.
		if _, err := tx.ExecContext(ctx, "UPDATE OR REPLACE meta SET key=replace(key,?,?) WHERE key LIKE 'warn:%' OR key LIKE 'cleanup:%'", old, next); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE OR REPLACE meta SET key=replace(key,'low_disk-episode','low_disk_episode') WHERE key LIKE 'cleanup:%'"); err != nil {
		return err
	}
	// Keyset batches bound memory and preserve feed sequence/key identity. A
	// malformed retained payload aborts the transaction instead of losing data.
	var after int64
	for {
		rows, err := tx.QueryContext(ctx, "SELECT seq,kind,payload FROM feed WHERE seq>? ORDER BY seq LIMIT 100", after)
		if err != nil {
			return err
		}
		type row struct {
			seq           int64
			kind, payload string
		}
		batch := []row{}
		for rows.Next() {
			var r row
			if err = rows.Scan(&r.seq, &r.kind, &r.payload); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			break
		}
		for _, r := range batch {
			var data any
			d := json.NewDecoder(bytes.NewBufferString(r.payload))
			d.UseNumber()
			if err = d.Decode(&data); err != nil {
				return fmt.Errorf("migrate feed sequence %d: %w", r.seq, err)
			}
			data = canonPayload(data, r.kind)
			b, err := json.Marshal(data)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE feed SET payload=? WHERE seq=?", string(b), r.seq); err != nil {
				return err
			}
			after = r.seq
		}
	}
	return nil
}
func canonPayload(v any, kind string) any {
	switch v := v.(type) {
	case []any:
		for i := range v {
			v[i] = canonPayload(v[i], kind)
		}
	case map[string]any:
		out := map[string]any{}
		for key, x := range v {
			name := key
			if next, ok := canonFields[key]; ok {
				name = next
			}
			if key == "ts" {
				name = "occurred_at"
				if kind == "machine_sample" {
					name = "sampled_at"
				}
			}
			if n, ok := x.(json.Number); ok {
				if key == "age" || key == "last_seen_age" {
					i, _ := n.Int64()
					x = i / 1000000
				}
				if key == "age_seconds" || key == "cpu_seconds" {
					f, _ := strconv.ParseFloat(string(n), 64)
					x = int64(f * 1000)
				}
			}
			if text, ok := x.(string); ok {
				switch key {
				case "label", "state":
					if strings.HasPrefix(text, "~") {
						out["heuristic"] = true
					}
					text = strings.TrimSuffix(strings.TrimPrefix(text, "~"), "?")
					x = text
				}
				switch key {
				case "label", "state", "status", "kind":
					if next, ok := canonEnums[text]; ok {
						x = next
					}
				}
				if strings.HasSuffix(name, "_at") {
					if at, err := time.Parse(time.RFC3339Nano, text); err == nil {
						x = at.UTC().Format(time.RFC3339Nano)
					}
				}
			}
			out[name] = canonPayload(x, kind)
		}
		if label, ok := v["label"].(string); ok && strings.HasPrefix(label, "~") {
			out["heuristic"] = true
		}
		if state, ok := v["state"].(string); ok && strings.HasPrefix(state, "~") {
			out["heuristic"] = true
		}
		return out
	}
	return v
}
