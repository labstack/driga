package view

import (
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

// ActivityBucket holds the latest sample in a minute; nil is a collection gap.
type ActivityBucket struct {
	TS     time.Time            `json:"sampled_at"`
	Sample *model.MachineSample `json:"sample"`
	Events int                  `json:"events"`
}

// ActivityMachine keeps each machine's history and freshness independent.
type ActivityMachine struct {
	MachineFresh
	MachineID string           `json:"machine_id"`
	Buckets   []ActivityBucket `json:"buckets"`
}

// ActivityView contains sixty one-minute buckets per selected machine.
type ActivityView struct {
	Machines []ActivityMachine `json:"machines"`
}

// Activity reads machine-wide samples and events, including the current minute.
func Activity(r Reader, f Filter, now time.Time, scopes ...store.Scope) (ActivityView, error) {
	v := ActivityView{Machines: []ActivityMachine{}}
	start := now.UTC().Truncate(time.Minute).Add(-59 * time.Minute)
	err := machineReaders(r, scopeOf(f, scopes), func(mr Reader, m model.Machine) error {
		a := ActivityMachine{MachineID: m.MachineID, MachineFresh: freshness(m, r.MachineID(), now), Buckets: make([]ActivityBucket, 60)}
		for i := range a.Buckets {
			a.Buckets[i].TS = start.Add(time.Duration(i) * time.Minute)
		}
		samples, err := mr.Samples(start)
		if err != nil {
			return err
		}
		for _, s := range samples {
			if s.TS.After(now) {
				continue
			}
			i := int(s.TS.Sub(start) / time.Minute)
			if i >= 0 && i < len(a.Buckets) {
				a.Buckets[i].Sample = &s
			}
		}
		counts, err := mr.EventCounts(start, now)
		if err != nil {
			return err
		}
		for _, c := range counts {
			i := int(c.TS.Sub(start) / time.Minute)
			if i >= 0 && i < len(a.Buckets) {
				a.Buckets[i].Events = c.Count
			}
		}
		v.Machines = append(v.Machines, a)
		return nil
	})
	return v, err
}
