package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/model"
)

func TestToolIdentitySurvivesOffsetChanges(t *testing.T) {
	const at = "2026-10-18T12:00:06Z"
	const tool = `{"type":"function_call","name":"read_file","arguments":{"path":"private-project.go","line":1}}`
	identity := func(at, payload string, offset int64) string {
		t.Helper()
		var obs model.SessionObs
		if err := fold(&obs, entry{Timestamp: at, Type: "response_item", Payload: json.RawMessage(payload)}, offset, false); err != nil {
			t.Fatal(err)
		}
		if len(obs.Events) != 1 {
			t.Fatalf("expected one tool event, got %+v", obs.Events)
		}
		if strings.Contains(obs.Events[0].Kind, "private-project") || obs.Events[0].Summary != "" {
			t.Fatalf("identity retained private tool arguments: %+v", obs.Events[0])
		}
		return obs.Events[0].Kind
	}
	first := identity(at, tool, 100)
	if replay := identity(at, tool, 5000); replay != first {
		t.Fatalf("rotation changed identity: %s != %s", replay, first)
	}
	if reordered := identity(at, `{"arguments":{"line":1,"path":"private-project.go"}, "name":"read_file", "type":"function_call"}`, 6000); reordered != first {
		t.Fatalf("JSON field ordering changed identity: %s != %s", reordered, first)
	}
	if changed := identity(at, strings.ReplaceAll(tool, `"line":1`, `"line":2`), 100); changed == first {
		t.Fatal("distinct tool content reused identity")
	}
	if changed := identity("2026-10-18T12:00:07Z", tool, 100); changed == first {
		t.Fatal("distinct tool timestamp reused identity")
	}
}
