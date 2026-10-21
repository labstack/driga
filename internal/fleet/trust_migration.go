package fleet

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

// migrateViewerContract rewrites the unreleased v1 trust-file key before the
// current reader decodes it. New readers and writers use only last_connected_at.
func migrateViewerContract(path string, b []byte) ([]byte, error) {
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, err
	}
	changed := false
	for _, row := range rows {
		if old, ok := row["last_connected"]; ok {
			if _, exists := row["last_connected_at"]; !exists {
				row["last_connected_at"] = old
			}
			delete(row, "last_connected")
			changed = true
		}
		for key, raw := range row {
			if !strings.HasSuffix(key, "_at") {
				continue
			}
			var at time.Time
			if err := json.Unmarshal(raw, &at); err != nil {
				return nil, err
			}
			normalized, err := json.Marshal(at.UTC())
			if err != nil {
				return nil, err
			}
			if !bytes.Equal(raw, normalized) {
				row[key] = normalized
				changed = true
			}
		}
	}
	if !changed {
		return b, nil
	}
	next, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return nil, err
	}
	next = append(next, '\n')
	if err = atomicPrivateWrite(path, next); err != nil {
		return nil, err
	}
	return next, nil
}
