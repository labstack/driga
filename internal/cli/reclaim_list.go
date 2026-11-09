package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/reclaim"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

type ReclaimListCmd struct {
	JSONOptions
	Sizes bool `help:"Measure disk candidates on demand (30s cap per walk)."`
	Why   bool `help:"Include disk candidates that require review or are protected."`
}

func (c ReclaimListCmd) Run(g *Globals) error {
	ctx := context.Background()
	local := *g
	local.Machine = "local"
	st, mode, closeFn, err := OpenReader(ctx, &local)
	if err != nil {
		return err
	}
	defer closeFn()
	env, _, closeCleanup, err := openCleanup(ctx, &local)
	if err != nil {
		return err
	}
	defer closeCleanup()
	env.Sizes, env.Why = c.Sizes, c.Why
	disk, err := cleanup.List(ctx, env)
	if err != nil {
		return err
	}
	envelope, err := readReclaimPlan(ctx, st, mode, disk)
	if err != nil {
		return err
	}
	rows := envelope.Data.([]reclaim.Opportunity)
	if g.JSON {
		return json.NewEncoder(commandOutput(g)).Encode(envelope)
	}
	w := tabwriter.NewWriter(commandOutput(g), 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tRESOURCE\tSTATUS\tCPU\tMEMORY\tMEASURE\tRSS\tDISK\tNEXT STEP\tREASON")
	for _, r := range rows {
		cpu, memory, rss, disk := "—", "—", "—", "—"
		if r.ResourcesKnown {
			cpu = fmt.Sprintf("%.2f%%", r.CPU)
			memory = view.HumanBytes(r.Memory.Memory)
			rss = view.HumanBytes(r.Memory.RSS)
		}
		if r.DiskKnown {
			disk = view.HumanBytes(uint64(max(0, r.DiskBytes)))
		}
		fmt.Fprintf(w, "%q\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%q\n", r.ID, r.Kind, r.Status, cpu, memory, r.Memory.Measure, rss, disk, r.Action, r.Reason)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(commandOutput(g), "observed_at: %s (%s)\n", envelope.ObservedAt.Format(time.RFC3339), envelope.Mode); err != nil {
		return err
	}
	var stale []string
	for name, f := range envelope.Freshness {
		if f.Stale {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	for _, name := range stale {
		if _, err := fmt.Fprintf(commandOutput(g), "stale: %s (%s ago)\n", name, envelope.Freshness[name].LastOKAt.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(commandOutput(g), "Estimates use collected samples; CPU 100% is one core. Footprint may include compressed/swapped pages. Actions recheck safety before applying. Use --sizes for disk estimates; driga reclaim history shows outcomes.")
	return err
}

// Read one committed process/session frame after potentially slow disk inspection.
func readReclaimPlan(ctx context.Context, st *store.Store, mode string, disk []cleanup.Candidate) (view.Envelope, error) {
	var envelope view.Envelope
	err := st.Tx(ctx, func(tx *store.Tx) error {
		now := time.Now()
		scope := store.Scope{Machines: []string{tx.MachineID()}}
		os, err := view.Orphans(tx, view.Filter{Scope: scope}, now)
		if err != nil {
			return err
		}
		procs, err := tx.Procs()
		if err != nil {
			return err
		}
		ss, err := tx.Sessions(store.SessionFilter{IncludeEnded: true})
		if err != nil {
			return err
		}
		ms, err := tx.Machines()
		if err != nil {
			return err
		}
		sourceOS := ""
		for _, m := range ms {
			if m.MachineID == tx.MachineID() {
				sourceOS = m.OS
			}
		}
		var idx derive.ProcIndex
		if len(procs) > 0 {
			idx, err = derive.NewProcIndexForOS(procs, sourceOS)
			if err != nil {
				return err
			}
		}
		rows := reclaim.Opportunities(disk, os.Rows, ss, idx)
		envelope, err = view.NewEnvelope(tx, mode, time.Now(), collector.Cadence(0), rows, scope)
		return err
	})
	return envelope, err
}
