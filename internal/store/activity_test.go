package store

import (
	"strings"
	"testing"
	"time"
)

// Activity counts events by machine and time on every refresh; without an
// index starting with (machine_id, ts) that reads a month of events.
func TestEventCountsUseTimeIndex(t *testing.T) {
	st, err := Open("", Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.db.Query(`EXPLAIN QUERY PLAN SELECT COUNT(*) FROM events WHERE machine_id=? AND ts>=? AND ts<=?`, "m", 0, time.Now().UnixNano())
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail + "\n")
	}
	if !strings.Contains(plan.String(), "events_machine_ts") {
		t.Fatalf("plan: %s", plan.String())
	}
}
