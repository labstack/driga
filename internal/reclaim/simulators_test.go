package reclaim

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSimulatorInventoryReportsEveryBootedDeviceForReview(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	const fixture = `{"devices":{"runtime-b":[{"udid":"six","name":"Phone six","state":"Booted","isAvailable":true},{"udid":"five","name":"Phone five","state":"Booted"},{"udid":"four","name":"Phone four","state":"Booted"}],"runtime-a":[{"udid":"three","name":"Phone three","state":"Booted"},{"udid":"two","name":"Phone two","state":"Booted"},{"udid":"one","name":"Ordinary device name","state":"Booted","lastBootedAt":"2026-11-08T12:34:56.123Z"},{"udid":"shutdown","state":"Shutdown"},{"udid":"unavailable","state":"Booted","isAvailable":false}]}}`
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rows, err := CollectSimulators(ctx, func(got context.Context, binary string, args ...string) ([]byte, error) {
		calls++
		if got != ctx || binary != "xcrun" || !reflect.DeepEqual(args, []string{"simctl", "list", "devices", "booted", "--json"}) {
			t.Fatalf("unexpected invocation: %q %q", binary, args)
		}
		return []byte(fixture), nil
	}, now)
	if err != nil || calls != 1 || len(rows) != 6 {
		t.Fatalf("booted device inventory = %+v, calls=%d, err=%v", rows, calls, err)
	}
	wantIDs := []string{"five", "four", "one", "six", "three", "two"}
	for i, row := range rows {
		if row.ID != wantIDs[i] || row.Status != "needs_review" || row.State != "Booted" || !row.ObservedAt.Equal(now) || !strings.Contains(row.Reason, "active use") {
			t.Fatalf("invented authorization or incomplete inventory: %+v", row)
		}
		if row.ID == "one" {
			if row.LastBootedAt == nil || row.LastBootedAt.Format(time.RFC3339Nano) != "2026-11-08T12:34:56.123Z" || row.Runtime != "runtime-a" {
				t.Fatalf("boot observation lost: %+v", row)
			}
		} else if row.LastBootedAt != nil || !strings.Contains(row.Reason, "Last boot time unavailable") {
			t.Fatalf("invented boot age: %+v", row)
		}
	}
}

func TestSimulatorInventoryRejectsInvalidObservations(t *testing.T) {
	for _, input := range []string{
		`not json`, `null`, `{}`, `{"devices":null}`, `{"devices":[]}`,
		`{"devices":{"runtime":null}}`, `{"devices":{"runtime":[null]}}`,
		`{"devices":{"runtime":[{"udid":"a"}]}}`,
		`{"devices":{"runtime":[{"state":"Booted"}]}}`,
		`{"devices":{"runtime":[{"udid":"same","state":"Booted"},{"udid":"same","state":"Booted"}]}}`,
		`{"devices":{"runtime":[{"udid":"a","state":"Booted","isAvailable":"yes"}]}}`,
		`{"devices":{}} {"devices":{}}`, strings.Repeat(" ", (4<<20)+1),
	} {
		t.Run(input[:min(len(input), 60)], func(t *testing.T) {
			rows, err := parseSimulators([]byte(input), time.Now())
			if err == nil || rows != nil {
				t.Fatalf("invalid inventory accepted: %+v, %v", rows, err)
			}
		})
	}
}

func TestSimulatorInventoryKeepsInvalidBootTimeUnknown(t *testing.T) {
	for _, timestamp := range []string{`"bad timestamp"`, `null`, `42`} {
		rows, err := parseSimulators([]byte(`{"devices":{"runtime":[{"udid":"a","state":"Booted","lastBootedAt":`+timestamp+`}]}}`), time.Now())
		if err != nil || len(rows) != 1 || rows[0].LastBootedAt != nil || !strings.Contains(rows[0].Reason, "Last boot time unavailable") {
			t.Fatalf("invalid boot time must remain unknown: %+v, %v", rows, err)
		}
	}
	rows, err := parseSimulators([]byte(`{"devices":{}}`), time.Now())
	if err != nil || rows == nil || len(rows) != 0 {
		t.Fatalf("valid empty inventory: %+v, %v", rows, err)
	}
}

func TestSimulatorInventoryDoesNotConfuseLastUseWithBootOrIdleness(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	rows, err := parseSimulators([]byte(`{"devices":{"runtime":[{"udid":"synthetic-device","name":"Fixture phone","state":"Booted","lastUsedAt":"2026-11-05T17:48:14Z"}]}}`), now)
	if err != nil || len(rows) != 1 {
		t.Fatalf("last-use inventory: %+v, %v", rows, err)
	}
	row := rows[0]
	if row.LastUsedAt == nil || row.LastUsedAt.Format(time.RFC3339) != "2026-11-05T17:48:14Z" || row.LastBootedAt != nil || row.Status != "needs_review" || !strings.Contains(row.Reason, "active use and session ownership are unknown") || !strings.Contains(row.Reason, "Last boot time unavailable") {
		t.Fatalf("last use supplied boot time or shutdown authority: %+v", row)
	}
	for _, timestamp := range []string{`"invalid"`, `null`, `42`} {
		rows, err := parseSimulators([]byte(`{"devices":{"runtime":[{"udid":"synthetic-device","state":"Booted","lastUsedAt":`+timestamp+`}]}}`), now)
		if err != nil || len(rows) != 1 || rows[0].LastUsedAt != nil || rows[0].LastBootedAt != nil {
			t.Fatalf("invalid last use must stay unknown: %+v, %v", rows, err)
		}
	}
}

func TestSimulatorInventoryFailureDoesNotLeakToolOutput(t *testing.T) {
	const secret = "private raw command error"
	rows, err := CollectSimulators(context.Background(), func(context.Context, string, ...string) ([]byte, error) {
		return []byte(secret), errors.New(secret)
	}, time.Now())
	if rows != nil || err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("tool failure leaked output or produced inventory: %+v, %v", rows, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rows, err = CollectSimulators(ctx, func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("cancelled collection invoked runner")
		return nil, nil
	}, time.Now())
	if rows != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inventory = %+v, %v", rows, err)
	}
}
