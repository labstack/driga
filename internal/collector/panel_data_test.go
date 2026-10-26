package collector

import (
	"context"
	"github.com/labstack/driga/internal/model"
	"testing"
	"time"
)

type cpuPlatform struct {
	*fakePlatform
	sample model.MachineSample
}

func (p *cpuPlatform) Machine(context.Context) (model.MachineSample, error) { return p.sample, nil }

func TestPassPersistsPanelHistoryAndCPU(t *testing.T) {
	d, st, p, a := setup(t)
	at := d.now()
	cpu := &cpuPlatform{fakePlatform: p, sample: model.MachineSample{TS: at, CPUTotalSeconds: 1000, CPUBusySeconds: 400, DiskTotal: 500, DiskFree: 100}}
	d.Plat = cpu
	a.obs[0].Events = []model.Event{{TS: at, Kind: "log:turn_end"}, {TS: at, Kind: "log:tool_call:fixture", Metadata: &model.EventMetadata{Tool: "Edit"}}}
	pass(t, d, st, Plan{Machine: true, Procs: true})
	first, err := st.LatestSample()
	if err != nil || first.CPUPercent != nil {
		t.Fatalf("first %+v %v", first, err)
	}
	at = at.Add(5 * time.Second)
	d.Now = func() time.Time { return at }
	cpu.sample.TS = at
	cpu.sample.CPUTotalSeconds = 1040
	cpu.sample.CPUBusySeconds = 410
	a.obs[0].Events = nil
	pass(t, d, st, Plan{Machine: true, Procs: true})
	sample, err := st.LatestSample()
	if err != nil || sample.CPUPercent == nil || *sample.CPUPercent != 25 || sample.DiskTotal != 500 {
		t.Fatalf("sample %+v %v", sample, err)
	}
	at = at.Add(time.Second)
	pass(t, d, st, Plan{})
	polls, err := st.PanelPolls(at)
	if err != nil || len(polls) != 2 {
		t.Fatalf("polls %+v %v", polls, err)
	}
	events, err := st.PanelEvents("claude", "one", time.Time{})
	if err != nil || len(events) != 2 {
		t.Fatalf("metadata not retained %+v %v", events, err)
	}
}
