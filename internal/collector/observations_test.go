package collector

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPassObservations(t *testing.T) {
	d, st, p, a := setup(t)
	path := filepath.Join(t.TempDir(), "transcript")
	a.obs[0].SourcePath = path
	write := func(n int) {
		t.Helper()
		if err := os.WriteFile(path, make([]byte, n), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(100)
	now := d.Now()
	d.Now = func() time.Time { return now }
	run := func(wantRows int, wantBytes, wantCPU int64, wantSamples int) {
		t.Helper()
		pass(t, d, st, Plan{Procs: true})
		rows, err := st.ObservationMinutes(sessions(t, st)[0].ID, now)
		if err != nil || len(rows) != wantRows {
			t.Fatalf("rows=%+v err=%v", rows, err)
		}
		if wantRows > 0 {
			r := rows[0]
			if r.LogBytes != wantBytes || r.CPUMS != wantCPU || r.Samples != wantSamples {
				t.Fatalf("row=%+v", r)
			}
		}
		now = now.Add(time.Second)
	}
	run(0, 0, 0, 0) // First sight establishes baselines; no historical bytes or CPU.
	write(112)
	p.procs[0].CPUSeconds += .25
	run(1, 12, 250, 1)
	run(1, 12, 250, 1) // Idle pass has no observation write.
	write(2)
	run(1, 12, 250, 1) // Truncation resets baseline.
	write(5)
	run(1, 15, 250, 2)
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	write(500)
	run(1, 15, 250, 2) // Larger replacement is rotation, not growth.
	write(504)
	run(1, 19, 250, 3)
	p.procs[0].StartTime = now
	a.obs[0].ProcStart = now
	p.procs[0].CPUSeconds = 100
	run(1, 19, 250, 3) // PID reuse cannot invent CPU.
	t.Log("fixture: 8 passes, 3 observation upserts (0.375/pass); idle/truncation/rotation/PID reset: 0 writes/pass")
}

func TestPassDoesNotAttributeSharedProcessCPU(t *testing.T) {
	d, st, p, a := setup(t)
	shared := a.obs[0]
	shared.ExternalID = "shared-host"
	a.obs = append(a.obs, shared)
	pass(t, d, st, Plan{Procs: true})
	p.procs[0].CPUSeconds += .25
	pass(t, d, st, Plan{Procs: true})
	for _, s := range sessions(t, st) {
		rows, err := st.ObservationMinutes(s.ID, d.Now())
		if err != nil || len(rows) > 0 {
			t.Fatalf("attributed shared process CPU to %s: %+v %v", s.ExternalID, rows, err)
		}
	}
}

func TestPassCPUMillisecondsDoNotLoseFractionalDeltas(t *testing.T) {
	d, st, p, _ := setup(t)
	pass(t, d, st, Plan{Procs: true})
	for i := 1; i <= 5; i++ {
		p.procs[0].CPUSeconds = 2 + float64(i)*.0004
		pass(t, d, st, Plan{Procs: true})
	}
	rows, err := st.ObservationMinutes(sessions(t, st)[0].ID, d.Now())
	if err != nil || len(rows) != 1 || rows[0].CPUMS != 2 {
		t.Fatalf("fractional CPU deltas lost: %+v %v", rows, err)
	}
}
