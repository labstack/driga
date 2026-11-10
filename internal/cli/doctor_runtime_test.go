package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/collector"
)

func TestDoctorAttributesRuntimeToFreshCollectorOnly(t *testing.T) {
	for _, variant := range []string{"current", "other pid", "prior run", "missing instance", "stale", "future", "unlocked"} {
		t.Run(variant, func(t *testing.T) {
			d, st := doctorFixture(t)
			d.LockProbe = func(string) (collector.Owner, bool, error) {
				return collector.Owner{PID: 1, Instance: "current"}, true, nil
			}
			s := collector.RuntimeStats{PID: 1, Instance: "current", At: d.Now, Phase: "after_process_pass", HeapLiveBytes: 8_000_000, HeapGoalBytes: 16_000_000, HeapAllocatedBytes: 10_000_000, GoSysBytes: 30_000_000, GoReleasedBytes: 5_000_000, GCCycles: 7, GCCPUFraction: 0.002}
			switch variant {
			case "other pid":
				s.PID = 2
			case "prior run":
				s.Instance = "previous"
			case "missing instance":
				s.Instance = ""
			case "stale":
				s.At = d.Now.Add(-time.Hour)
			case "future":
				s.At = d.Now.Add(time.Second)
			case "unlocked":
				d.LockProbe = func(string) (collector.Owner, bool, error) { return collector.Owner{PID: 1}, false, nil }
			}
			raw, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			if err := st.SetMeta("collector_runtime", string(raw)); err != nil {
				t.Fatal(err)
			}
			checks := Doctor(context.Background(), d)
			if variant == "current" {
				c := requireCheck(t, checks, "service_runtime", "info", "")
				for _, want := range []string{"live 8.0 MB", "goal 16.0 MB", "allocated 10.0 MB", "Go-managed 25.0 MB", "GC cycles 7", "not physical footprint", "after process pass"} {
					if !strings.Contains(c.Detail, want) {
						t.Fatalf("missing %q: %+v", want, c)
					}
				}
			} else {
				for _, c := range checks {
					if c.Name == "service_runtime" && strings.Contains(c.Detail, "live 8.0 MB") {
						t.Fatalf("%s attributed runtime to wrong collector: %+v", variant, c)
					}
				}
			}
		})
	}
}
