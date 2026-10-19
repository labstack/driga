package intake

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestClaudeCode21303Fixtures(t *testing.T) {
	wants := map[string][]string{
		"00_v1_logs.json":        {"mcp_server_connection"},
		"01_v1_logs.json":        {"mcp_server_connection"},
		"02_v1_logs.json":        {"user_prompt"},
		"03_v1_logs.json":        {"mcp_server_connection"},
		"04_v1_logs.json":        {"tool_decision", "api_request"},
		"05_v1_logs.json":        {"tool_result"},
		"06_v1_logs.json":        {"api_request", "assistant_response", "mcp_server_connection"},
		"encoding-variants.json": {"api_error", "user_prompt"},
	}
	ignored := map[string]int{"00_v1_logs.json": 5, "02_v1_logs.json": 2, "04_v1_logs.json": 2, "06_v1_logs.json": 2}
	for name, want := range wants {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("../../testdata/fixtures/otlp/claude-code-2.1.303", name))
			if err != nil {
				t.Fatal(err)
			}
			var got Batch
			w := request(t, raw, "application/json", "", func(_ context.Context, b Batch) (int, error) { got = b; return 0, nil })
			if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "{}" {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			var names []string
			for _, f := range got.Facts {
				names = append(names, f.Name)
				if f.SessionID != "fixture-session" || f.TS.IsZero() {
					t.Fatal(f)
				}
			}
			if !reflect.DeepEqual(names, want) || got.Rejected != 0 || got.Ignored != ignored[name] {
				t.Fatalf("got %+v, names %v; want %v, ignored %d", got, names, want, ignored[name])
			}
			stored, _ := json.Marshal(got)
			for _, content := range []string{"<REDACTED>", "NEVER_STORE", "synthetic-user", "synthetic-organization"} {
				if bytes.Contains(stored, []byte(content)) {
					t.Fatalf("retained content: %s", stored)
				}
			}
			if name == "encoding-variants.json" {
				if got.Facts[0].Summary != "API error 429, attempt 2" || got.Facts[0].TS.UnixNano() != 1792324800000000001 || got.Facts[1].TS.UnixNano() != 1792324800000000002 {
					t.Fatal(got)
				}
			}
		})
	}
}

func TestOTLPIgnoresUnsupportedRecords(t *testing.T) {
	for _, name := range []string{"hook_execution_start", "claude_code.future_event", "foreign.event"} {
		raw := strings.ReplaceAll(string(fixture(t)), "claude_code.tool_decision", name)
		b, err := Decode(strings.NewReader(raw), "/v1/logs")
		if err != nil || b.Rejected != 0 || b.Ignored != 1 || len(b.Facts) != 1 {
			t.Fatal(b, err)
		}
	}
}

func TestOTLPInvalidInteger(t *testing.T) {
	for _, v := range []string{`true`, `[]`, `"bad"`, `1.5`, `9223372036854775808`} {
		raw := `{"resourceLogs":[{"resource":{"attributes":[{"key":"event.sequence","value":{"intValue":` + v + `}}]}}]}`
		if w := request(t, []byte(raw), "application/json", "", nil); w.Code != 400 {
			t.Fatalf("%s: %d", v, w.Code)
		}
	}
}

func TestOTLPIntegerEncoding(t *testing.T) {
	for _, raw := range []string{`100`, `"100"`, `1e2`, `"1e2"`, `100.0`, `"100.0"`, `1000e-1`} {
		var signed int64Value
		var unsigned uint64Value
		if err := json.Unmarshal([]byte(raw), &signed); err != nil || signed != 100 {
			t.Fatalf("int64 %s: %d %v", raw, signed, err)
		}
		if err := json.Unmarshal([]byte(raw), &unsigned); err != nil || unsigned != 100 {
			t.Fatalf("fixed64 %s: %d %v", raw, unsigned, err)
		}
	}
	for _, raw := range []string{`18446744073709551615`, `"18446744073709551615"`} {
		var n uint64Value
		if err := json.Unmarshal([]byte(raw), &n); err != nil || n != ^uint64Value(0) {
			t.Fatal(raw, n, err)
		}
	}
	for _, raw := range []string{`-1`, `18446744073709551616`, `1.01`, `1e999999999`, `1e-999999999`, `true`} {
		var n uint64Value
		if err := json.Unmarshal([]byte(raw), &n); err == nil {
			t.Fatal("accepted invalid fixed64", raw, n)
		}
	}
}
