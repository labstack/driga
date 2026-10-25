package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

type AckOptions struct {
	ID  string
	All bool
}

// Run appends ack events and waits at most two seconds for daemon receipts.
func (c AckOptions) Ack(g Environment) error {
	if err := Selection(c.ID, c.All); err != nil {
		return err
	}
	targets := []model.HookEvent{{Event: "ack", Message: c.ID}}
	remoteNames := make(map[string]string)
	st, openErr := store.Open(filepath.Join(config.DataDir(), "driga.db"), store.ReadOnly)
	if c.All && openErr != nil {
		var closeFn func()
		var err error
		st, _, closeFn, err = g.OpenReader(context.Background())
		if err != nil {
			return err
		}
		defer closeFn()
	} else if openErr == nil {
		defer st.Close()
	}
	if st != nil {
		scope, err := g.ResolveMachine(st)
		if err != nil {
			return err
		}
		items, err := st.InboxScoped(time.Time{}, false, scope)
		if err != nil {
			return err
		}
		var selected []model.HookEvent
		for _, item := range items {
			if !c.All && item.ID != c.ID {
				continue
			}
			ev := model.HookEvent{Event: "ack", Message: item.ID}
			if item.MachineID != st.MachineID() {
				if err := fleet.CheckAckPermission(config.DataDir(), item.MachineID); err != nil {
					return err
				}
				ev.MachineID = item.MachineID
				remoteNames[item.MachineID] = item.MachineID
			}
			selected = append(selected, ev)
		}
		if !c.All && len(selected) > 1 {
			return fmt.Errorf("inbox id %q exists on multiple machines; select --machine", c.ID)
		}
		if c.All || len(selected) > 0 {
			targets = selected
		} else if scope.Machines != nil && len(scope.Machines) == 1 && scope.Machines[0] != st.MachineID() {
			return fmt.Errorf("unknown inbox id %q on %s", c.ID, scope.Machines[0])
		}
		machines, err := st.Machines()
		if err != nil {
			return err
		}
		for _, machine := range machines {
			if _, ok := remoteNames[machine.MachineID]; ok {
				remoteNames[machine.MachineID] = view.MachineName(machine)
			}
		}
	} else if openErr != nil {
		if _, err := os.Stat(filepath.Join(config.DataDir(), "driga.db")); !os.IsNotExist(err) {
			return openErr
		}
		if g.Machine != "" && g.Machine != "all" && g.Machine != "local" {
			return fmt.Errorf("unknown machine %q: this machine's Driga service is not running", g.Machine)
		}
	}

	var ids []string
	for _, ev := range targets {
		ev.TS = time.Now()
		if err := spool.Append(config.SpoolDir(), ev); err != nil {
			return err
		}
		if ev.MachineID == "" {
			ids = append(ids, ev.Message)
		} else if _, err := fmt.Fprintf(g.Stdout, "ack pending → %s\n", remoteNames[ev.MachineID]); err != nil {
			return err
		}
	}
	if len(targets) == 0 {
		_, err := fmt.Fprintln(g.Stdout, "no unacknowledged items")
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	lock, err := collector.Lock(config.DataDir(), collector.Owner{Kind: "manual"})
	if err == nil {
		if err := lock.Close(); err != nil {
			return err
		}
		_, err = fmt.Fprintln(g.Stdout, "daemon not running — ack recorded, applies on next collect")
		return err
	}
	if !errors.Is(err, collector.ErrAlreadyRunning) {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	receipts := make(chan string, len(ids))
	// Store.Open has a SQLite busy timeout. Keep even a blocked open within the
	// command's receipt deadline; the worker closes its own read-only connection.
	go pollAckReceipts(ctx, filepath.Join(config.DataDir(), "driga.db"), ids, receipts)
	for remaining := len(ids); remaining > 0; {
		select {
		case id := <-receipts:
			if _, err := fmt.Fprintf(g.Stdout, "acked %s\n", id); err != nil {
				return err
			}
			remaining--
		case <-ctx.Done():
			_, err := fmt.Fprintln(g.Stdout, "ack recorded; daemon has not confirmed yet")
			return err
		}
	}
	return nil
}

func pollAckReceipts(ctx context.Context, path string, ids []string, receipts chan<- string) {
	pending := make(map[string]bool, len(ids))
	for _, id := range ids {
		pending[id] = true
	}
	var st *store.Store
	defer func() {
		if st != nil {
			_ = st.Close()
		}
	}()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if st == nil {
			st, _ = store.Open(path, store.ReadOnly)
		}
		if st != nil {
			_ = st.Tx(ctx, func(tx *store.Tx) error {
				for id := range pending {
					if _, err := tx.Meta("ack:" + id); err == nil {
						receipts <- id
						delete(pending, id)
					}
				}
				return nil
			})
		}
		if len(pending) == 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
