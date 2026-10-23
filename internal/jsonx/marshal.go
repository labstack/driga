// Package jsonx defines the JSON boundary for owned Driga contracts.
package jsonx

import (
	"strings"
	"time"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

var marshalers = json.JoinMarshalers(
	json.MarshalFunc(func(v time.Time) ([]byte, error) { return v.UTC().MarshalJSON() }),
	json.MarshalFunc(func(v time.Duration) ([]byte, error) { return json.Marshal(v.Milliseconds()) }),
)

// Marshal preserves sorted object keys, null collections, and HTML-safe strings
// while keeping time conversions at the wire boundary.
func Marshal(v any) ([]byte, error) {
	b, err := json.Marshal(v, json.WithMarshalers(marshalers), json.Deterministic(true), json.FormatNilSliceAsNull(true), json.FormatNilMapAsNull(true), jsontext.EscapeForHTML(true), jsontext.EscapeForJS(true))
	if err != nil {
		return nil, err
	}
	value := jsontext.Value(b)
	err = value.Format(jsontext.ReorderRawObjects(true), jsontext.EscapeForHTML(true), jsontext.EscapeForJS(true))
	return value, err
}

// Object merges embedded contracts followed by explicit fields. Later fields
// take precedence, just as explicit Go fields shadow promoted fields.
func Object(parts ...any) ([]byte, error) {
	fields := make(map[string]jsontext.Value)
	for _, part := range parts {
		b, err := Marshal(part)
		if err != nil {
			return nil, err
		}
		var next map[string]jsontext.Value
		if err := json.Unmarshal(b, &next); err != nil {
			return nil, err
		}
		for key, value := range next {
			fields[key] = value
		}
	}
	return Marshal(fields)
}

// Label removes display punctuation and retains the inferred-state flag.
func Label(label string, heuristic bool) (string, bool) {
	return strings.TrimSuffix(strings.TrimPrefix(label, "~"), "?"), heuristic || strings.HasPrefix(label, "~")
}
