package view

import (
	"encoding/json"
	"github.com/labstack/driga/internal/model"
	"strings"
	"testing"
	"time"
)

func TestJSONContract(t *testing.T) {
	at := time.Date(2026, 10, 19, 12, 0, 0, 123, time.FixedZone("offset", -7*3600))
	e := Envelope{Schema: 2, ObservedAt: at, Data: AgentRow{Label: "stalled", Heuristic: true, Age: 1500 * time.Millisecond, Mem: 123, LastActivityAt: at}}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"schema":2`, `"age_ms":1500`, `"mem_bytes":123`, `"heuristic":true`, `"label":"stalled"`, `2026-10-19T19:00:00.000000123Z`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s in %s", want, b)
		}
	}
	b, err = json.Marshal(model.MachineSample{TS: at, MemUsed: 17})
	if err != nil || !strings.Contains(string(b), `"sampled_at"`) || !strings.Contains(string(b), `"mem_used_bytes":17`) || strings.Contains(string(b), "-07:00") {
		t.Fatalf("sample: %s %v", b, err)
	}
	b, err = json.Marshal(model.Proc{CPUSeconds: 1.234, StartTime: at, RSS: 17})
	if err != nil || !strings.Contains(string(b), `"cpu_ms":1234`) {
		t.Fatalf("proc %s %v", b, err)
	}
	var proc model.Proc
	if err = json.Unmarshal(b, &proc); err != nil || proc.CPUSeconds != 1.234 || proc.RSS != 17 {
		t.Fatalf("proc roundtrip %+v %v", proc, err)
	}
}
