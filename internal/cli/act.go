package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/kill"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

// OrphansKillCmd terminates explicitly confirmed orphan trees.
type OrphansKillCmd struct {
	ID    string `arg:"" optional:"" help:"Orphan candidate ID."`
	All   bool   `help:"Ask for confirmation for each candidate."`
	Force bool   `help:"Also SIGKILL survivors, with a second confirmation."`
}

// InboxAckCmd records explicit acknowledgments through the collector's spool.
type InboxAckCmd struct {
	ID  string `arg:"" optional:"" help:"Inbox item ID."`
	All bool   `help:"Acknowledge every unacknowledged item."`
}

func actionSelection(id string, all bool) error {
	if id == "" && !all || id != "" && all {
		return fmt.Errorf("provide an id or --all")
	}
	return nil
}

// Run displays and confirms candidates, then revalidates each tree before kill.
// actPlatform and verifyWindow are replaced in tests.
var (
	actPlatform  = platform.New
	verifyWindow = 3 * time.Second
)

func (c OrphansKillCmd) Run(g *Globals) error {
	if err := actionSelection(c.ID, c.All); err != nil {
		return err
	}
	ctx := context.Background()
	st, _, closeFn, err := OpenReader(ctx, g)
	if err != nil {
		return err
	}
	defer closeFn()
	orphans, err := st.Orphans()
	if err != nil {
		return err
	}
	var candidates []model.Orphan
	for _, o := range orphans {
		if c.All || o.ID == c.ID {
			candidates = append(candidates, o)
		}
	}
	if !c.All && len(candidates) == 0 {
		return fmt.Errorf("unknown orphan id %q", c.ID)
	}
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	procs, err := st.Procs()
	if err != nil {
		return err
	}
	return c.killCandidates(ctx, g, cfg, procs, candidates)
}

func (c OrphansKillCmd) killCandidates(ctx context.Context, g *Globals, cfg config.Config, procs []model.Proc, candidates []model.Orphan) error {
	idx := derive.NewProcIndex(procs)
	in := g.Stdin
	if in == nil {
		in = strings.NewReader("")
	}
	input := bufio.NewReader(in)
	var errs []error
	for _, o := range candidates {
		if o.Young {
			err := fmt.Errorf("too young to kill (age %s, threshold %s)", max(time.Duration(0), time.Since(o.StartTime)).Round(time.Second), cfg.OrphanMinAge.Round(time.Second))
			if !c.All {
				return err
			}
			if _, err := fmt.Fprintf(g.Stdout, "%s: %s\n", o.ID, err); err != nil {
				return err
			}
			continue
		}
		pids := o.PIDs
		if len(pids) == 0 {
			pids = []int32{o.RootPID}
			for _, p := range idx.Descendants(o.RootPID) {
				pids = append(pids, p.PID)
			}
		}
		var expect []int32
		for _, pid := range pids {
			if pid != o.RootPID {
				expect = append(expect, pid)
			}
		}
		if !o.Verified {
			// One snapshot can't show idleness over time; check it now rather
			// than requiring the background service (spec §8.1 journey 5).
			if _, err := fmt.Fprintf(g.Stdout, "%s: checking it stays idle for %s…\n", o.ID, verifyWindow); err != nil {
				return err
			}
			ok, reason, err := kill.VerifyIdle(ctx, actPlatform(), o, expect, verifyWindow)
			if err == nil && !ok {
				err = errors.New("not idle: " + reason)
			}
			if err != nil {
				if !c.All {
					return err
				}
				if _, err := fmt.Fprintf(g.Stdout, "%s: %s\n", o.ID, err); err != nil {
					return err
				}
				continue
			}
			if _, err := fmt.Fprintf(g.Stdout, "%s: verified idle\n", o.ID); err != nil {
				return err
			}
		}
		command := "driga orphans kill " + o.ID
		if c.Force {
			command += " --force"
		}
		if _, err := fmt.Fprintln(g.Stdout, command); err != nil {
			return err
		}
		w := tabwriter.NewWriter(g.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "PID\tCMD\tRSS")
		for _, pid := range pids {
			p, _ := idx.Get(pid)
			fmt.Fprintf(w, "%d\t%s\t%d\n", pid, strings.Join(strings.Fields(p.Cmdline), " "), p.RSS)
		}
		if err := w.Flush(); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.Stdout, "~reclaim %d bytes (RSS estimate)\n", o.RSSTotal); err != nil {
			return err
		}
		yes, err := confirmAction(input, g.Stdout, "Kill "+o.ID+"? [y/N]")
		if err != nil {
			return err
		}
		if !yes {
			continue
		}
		if c.Force {
			yes, err = confirmAction(input, g.Stdout, "Really SIGKILL survivors? [y/N]")
			if err != nil {
				return err
			}
			if !yes {
				continue
			}
		}
		result, killErr := kill.Tree(ctx, actPlatform(), o, expect, c.Force, 5*time.Second)
		if result.Aborted {
			killErr = fmt.Errorf("kill %s aborted: %s", o.ID, result.Reason)
		}
		if _, err := fmt.Fprintf(g.Stdout, "signaled %v; survivors %v\n", result.Signaled, result.Survivors); err != nil {
			return err
		}
		if killErr != nil {
			errs = append(errs, killErr)
		}
	}
	return errors.Join(errs...)
}

func confirmAction(input *bufio.Reader, out io.Writer, prompt string) (bool, error) {
	if _, err := fmt.Fprint(out, prompt+" "); err != nil {
		return false, err
	}
	line, err := input.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(line), "y"), nil
}

// Run appends ack events and waits at most two seconds for daemon receipts.
func (c InboxAckCmd) Run(g *Globals) error {
	if err := actionSelection(c.ID, c.All); err != nil {
		return err
	}
	ids := []string{c.ID}
	if c.All {
		st, _, closeFn, err := OpenReader(context.Background(), g)
		if err != nil {
			return err
		}
		defer closeFn()
		items, err := st.Inbox(time.Time{}, false)
		if err != nil {
			return err
		}
		ids = nil
		for _, item := range items {
			ids = append(ids, item.ID)
		}
	}
	for _, id := range ids {
		if err := spool.Append(config.SpoolDir(), model.HookEvent{Event: "ack", Message: id, TS: time.Now()}); err != nil {
			return err
		}
	}
	if len(ids) == 0 {
		_, err := fmt.Fprintln(g.Stdout, "no unacknowledged items")
		return err
	}
	lock, err := collector.Lock(config.DataDir())
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
