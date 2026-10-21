// Package jsonwire serializes owned contracts with UTC instants and millisecond durations.
package jsonwire

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Marshal honors JSON field tags and embedding while converting time values at
// the boundary. It never changes the original view used by human renderers.
func Marshal(v any) ([]byte, error) { return json.Marshal(value(reflect.ValueOf(v))) }

func value(v reflect.Value) any {
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		return value(v.Elem())
	}
	if v.Type() == reflect.TypeFor[time.Time]() {
		return v.Interface().(time.Time).UTC()
	}
	if v.Type() == reflect.TypeFor[time.Duration]() {
		return v.Interface().(time.Duration).Milliseconds()
	}
	if v.Type() == reflect.TypeFor[json.RawMessage]() {
		return v.Interface()
	}
	switch v.Kind() {
	case reflect.Struct:
		out := map[string]any{}
		t := v.Type()
		// Embedded fields first; explicit fields take precedence as in encoding/json.
		for pass := 0; pass < 2; pass++ {
			for i := 0; i < v.NumField(); i++ {
				f := t.Field(i)
				if f.PkgPath != "" || (pass == 0) != f.Anonymous {
					continue
				}
				parts := strings.Split(f.Tag.Get("json"), ",")
				name := parts[0]
				if name == "-" {
					continue
				}
				field := v.Field(i)
				if len(parts) > 1 && parts[1] == "omitzero" && field.IsZero() {
					continue
				}
				if len(parts) > 1 && parts[1] == "omitempty" && empty(field) {
					continue
				}
				if f.Anonymous && name == "" {
					if fields, ok := value(field).(map[string]any); ok {
						for k, x := range fields {
							out[k] = x
						}
					}
					continue
				}
				if name == "" {
					name = f.Name
				}
				if name == "cpu_ms" && field.Kind() == reflect.Float64 {
					out[name] = int64(field.Float() * 1000)
				} else {
					out[name] = value(field)
				}
			}
		}
		// View labels are friendly internally. Punctuation and heuristic markers
		// belong only to the human display, never to the state enum on the wire.
		if label, ok := out["label"].(string); ok {
			if strings.HasPrefix(label, "~") {
				out["heuristic"] = true
			}
			out["label"] = strings.TrimSuffix(strings.TrimPrefix(label, "~"), "?")
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return nil
		}
		out := map[string]any{}
		iter := v.MapRange()
		for iter.Next() {
			out[fmt.Sprint(iter.Key().Interface())] = value(iter.Value())
		}
		return out
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil
		}
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return v.Interface()
		}
		out := make([]any, v.Len())
		for i := range out {
			out[i] = value(v.Index(i))
		}
		return out
	default:
		return v.Interface()
	}
}
func empty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Struct:
		return false
	default:
		return v.IsZero()
	}
}
