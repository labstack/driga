package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/reclaim"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

type ReclaimListCmd struct {
	JSONOptions
	Sizes      bool   `help:"Measure disk candidates on demand (30s cap per walk)."`
	Why        bool   `help:"Include disk candidates that require review or are protected."`
	Processes  bool   `help:"Review local detached processes without scanning disk."`
	Simulators bool   `help:"Review booted iOS simulators without scanning disk."`
	PID        *int32 `help:"Inspect one current process, including protections and missing evidence."`
}

func (c ReclaimListCmd) Run(g *Globals) error {
	if c.PID != nil && *c.PID <= 0 {
		return invalidUsage("--pid requires a positive PID")
	}
	if c.PID != nil && c.Simulators {
		return invalidUsage("--pid cannot be combined with --simulators")
	}
	fast := c.Processes || c.Simulators || c.PID != nil
	if fast && c.Sizes {
		return invalidUsage("--sizes requires disk review; omit --processes, --simulators and --pid")
	}
	if c.Simulators && runtime.GOOS != "darwin" && g.ReclaimTool == nil {
		return invalidUsage("iOS simulator inventory is available on macOS")
	}
	ctx, stop := notifyContext(context.Background())
	defer stop()
	local := *g
	local.Machine = "local"
	st, mode, closeFn, err := openReclaimReader(ctx, &local, fast)
	if err != nil {
		return err
	}
	defer closeFn()
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	var disk []cleanup.Candidate
	if !fast {
		env, _, closeCleanup, err := openCleanup(ctx, &local)
		if err != nil {
			return err
		}
		defer closeCleanup()
		env.Sizes, env.Why = c.Sizes, c.Why
		disk, err = cleanup.List(ctx, env)
		if err != nil {
			return err
		}
	}
	plat := g.CleanupPlatform
	if plat == nil {
		plat = platform.New()
	}
	inspection := reclaimInspection{Platform: plat, Processes: !fast || c.Processes || c.PID != nil,
		Simulators: c.Simulators || !fast && runtime.GOOS == "darwin", Tool: g.ReclaimTool, MinAge: cfg.OrphanMinAge}
	if c.PID != nil {
		inspection.PID = *c.PID
	}
	envelope, err := readReclaimPlanWithInspection(ctx, st, mode, disk, inspection)
	if err != nil {
		return err
	}
	rows := envelope.Data.([]reclaim.Opportunity)
	if g.JSON {
		return json.NewEncoder(commandOutput(g)).Encode(envelope)
	}
	w := tabwriter.NewWriter(commandOutput(g), 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tPID\tWHAT\tRESOURCE\tSTATUS\tAGE\tLAST USED\tCPU\tMEMORY\tMEASURE\tRSS\tDISK\tNEXT STEP\tREASON")
	for _, r := range rows {
		cpu, memory, rss, disk := "—", "—", "—", "—"
		if r.ResourcesKnown {
			if r.CPUKnown == nil || *r.CPUKnown {
				cpu = fmt.Sprintf("%.2f%%", r.CPU)
			}
			memory = view.HumanBytes(r.Memory.Memory)
			rss = view.HumanBytes(r.Memory.RSS)
		}
		if r.DiskKnown {
			disk = view.HumanBytes(uint64(max(0, r.DiskBytes)))
		}
		pid, age, used := "—", "—", "—"
		if r.RootPID > 0 {
			pid = fmt.Sprint(r.RootPID)
		}
		if !r.StartedAt.IsZero() {
			age = time.Since(r.StartedAt).Round(time.Minute).String()
		}
		if r.Simulator != nil && r.Simulator.LastUsedAt != nil {
			used = r.Simulator.LastUsedAt.Format(time.RFC3339)
		}
		fmt.Fprintf(w, "%q\t%s\t%q\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%q\n", r.ID, pid, r.Name, r.Kind, r.Status, age, used, cpu, memory, r.Memory.Measure, rss, disk, r.Action, r.Reason)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if c.PID != nil {
		for _, r := range rows {
			if r.RootPID == *c.PID && r.Cwd != "" {
				if _, err := fmt.Fprintf(commandOutput(g), "Working directory: %q\n", r.Cwd); err != nil {
					return err
				}
			}
			if r.RootPID == *c.PID && r.Kind == "process_review" && r.Status != "not_found" {
				if _, err := fmt.Fprintln(commandOutput(g), "Next step: review this process in its original session or owning tool; this inspection does not authorize shutdown."); err != nil {
					return err
				}
			}
		}
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
		last := "no successful observation"
		if !envelope.Freshness[name].LastOKAt.IsZero() {
			last = "last success " + envelope.Freshness[name].LastOKAt.Format(time.RFC3339)
		}
		if _, err := fmt.Fprintf(commandOutput(g), "stale: %s (%s)\n", name, last); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(commandOutput(g), "Process review and simulator observations do not authorize shutdown. CPU is the last known sample; 100% is one core. Footprint may include compressed/swapped pages. Actions recheck safety before applying. Use --pid for exact process inspection; --sizes for disk estimates; driga reclaim history shows outcomes.")
	return err
}

// Read one committed process/session frame after potentially slow disk inspection.
func readReclaimPlan(ctx context.Context, st *store.Store, mode string, disk []cleanup.Candidate) (view.Envelope, error) {
	return readReclaimPlanWithInspection(ctx, st, mode, disk, reclaimInspection{})
}

func readReclaimPlanWithInspection(ctx context.Context, st *store.Store, mode string, disk []cleanup.Candidate, inspection reclaimInspection) (view.Envelope, error) {
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
		if inspection.Processes {
			rows, err = inspectReclaimProcesses(ctx, tx, inspection, rows, procs, ss, os.Rows)
			if err != nil {
				return err
			}
		} else if inspection.Simulators {
			rows = []reclaim.Opportunity{}
		}
		if inspection.Simulators {
			rows = appendReclaimSimulators(ctx, inspection, rows)
		}
		reclaim.SortOpportunities(rows)
		envelope, err = view.NewEnvelope(tx, mode, time.Now(), collector.Cadence(0), rows, scope)
		return err
	})
	return envelope, err
}
