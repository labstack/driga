package view

import (
	"encoding/json"
	"github.com/google/jsonschema-go/jsonschema"
	"os"
	"testing"
)

func TestDocumentedSchema2Records(t *testing.T) {
	b, err := os.ReadFile("../../docs/contracts/schema2.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err = json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		record string
		valid  bool
	}{
		{`{"schema":2,"observed_at":"2026-10-19T19:00:00Z","mode":"daemon","freshness":{},"coverage_gaps":[],"data":{}}`, true},
		{`{"schema":2,"error":{"code":"runtime_error","message":"unavailable"}}`, true},
		{`{"schema":1,"data":{}}`, false},
		{`{"schema":2,"observed_at":"2026-10-19T12:00:00-07:00","data":{}}`, false},
		{`{"schema":2,"error":{"code":"runtime-error","message":"unavailable"}}`, false},
		{`{"schema":2,"data":{"rows":[{"id":"w","session_id":1,"kind":"waiting","occurred_at":"2026-10-19T19:00:00Z","summary":"waiting for input","reason":"permission","source":"hook","waiting_since_at":"2026-10-19T19:00:00Z","waited_ms":1000}]}}`, true},
		{`{"schema":2,"data":{"rows":[{"id":"w","session_id":1,"kind":"waiting","occurred_at":"2026-10-19T19:00:00Z","summary":"waiting","reason":"guessed","source":"hook","waiting_since_at":"2026-10-19T19:00:00Z"}]}}`, false},
		{`{"schema":2,"data":{"rows":[{"id":"w","session_id":1,"kind":"waiting","occurred_at":"2026-10-19T19:00:00Z","summary":"waiting"}]}}`, false},
	} {
		var record any
		if err = json.Unmarshal([]byte(tc.record), &record); err != nil {
			t.Fatal(err)
		}
		if err = resolved.Validate(record); (err == nil) != tc.valid {
			t.Errorf("%s: valid=%v error=%v", tc.record, tc.valid, err)
		}
	}
}
