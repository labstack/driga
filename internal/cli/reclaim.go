package cli

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/reclaim"
	"github.com/labstack/driga/internal/view"
)

type ReclaimCmd struct {
	List    ReclaimListCmd    `cmd:"" default:"withargs" help:"Review local resource reclaim candidates and safety decisions."`
	History ReclaimHistoryCmd `cmd:"" help:"Show local process and disk action receipts."`
}

type ReclaimHistoryCmd struct {
	JSONOptions
	Kind  string `help:"Filter by process or disk."`
	Limit int    `default:"50" help:"Maximum receipts, newest first (1–1000)."`
}

func (c ReclaimHistoryCmd) Run(g *Globals) error {
	if c.Limit < 1 || c.Limit > 1000 {
		return invalidUsage("--limit must be between 1 and 1000")
	}
	if c.Kind != "" && c.Kind != "process" && c.Kind != "disk" {
		return invalidUsage("--kind must be process or disk")
	}
	history, err := reclaim.Read(config.DataDir(), c.Limit, c.Kind)
	if err != nil {
		return err
	}
	if g.JSON {
		return json.NewEncoder(commandOutput(g)).Encode(struct {
			Schema int             `json:"schema"`
			Data   reclaim.History `json:"data"`
		}{2, history})
	}
	w := tabwriter.NewWriter(commandOutput(g), 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "WHEN\tKIND\tID\tMACHINE\tSTATUS\tSTOPPED\tSURVIVORS\tUNKNOWN\tEXITED PRIOR MEMORY\tDISK FREED")
	for _, e := range history.Entries {
		memory := "—"
		freed := "—"
		if e.Kind == "process" && e.StoppedUsage.Processes > 0 {
			memory = view.HumanBytes(e.StoppedUsage.Memory) + " (" + e.StoppedUsage.Measure + ")"
		}
		if e.Kind == "disk" {
			freed = view.HumanBytes(uint64(max(0, e.FreedBytes)))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%d\t%d\t%s\t%s\n", e.At.Local().Format(time.RFC3339), e.Kind, e.ID, e.MachineID, e.Status, len(e.Stopped), len(e.Survivors), len(e.Unknown), memory, freed)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	for _, warning := range history.Warnings {
		if _, err := fmt.Fprintln(commandOutput(g), warning); err != nil {
			return err
		}
	}
	return nil
}
