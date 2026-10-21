package collector

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

// lowDiskInbox uses only this machine pass and cached attribution. It never
// walks sizes or runs cleanup's live git/process inspections in the collector.
func lowDiskInbox(tx *store.Tx, cfg config.MachineConfig, s model.MachineSample, now time.Time) error {
	if s.DiskTotal == 0 {
		return nil
	}
	threshold := cfg.DiskFreeMin
	if threshold == 0 {
		threshold = min(s.DiskTotal/10, uint64(20_000_000_000))
	}
	active, err := tx.Meta("cleanup:low_disk")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if s.DiskFree >= threshold {
		if err := tx.EndMachineWarning("low_disk", now); err != nil {
			return err
		}
		return tx.SetMeta("cleanup:low_disk", "0")
	}
	worktrees, err := tx.Worktrees(0)
	if err != nil {
		return err
	}
	sort.SliceStable(worktrees, func(i, j int) bool { return worktrees[i].LastCommitAt.Before(worktrees[j].LastCommitAt) })
	names := []string{}
	for _, w := range worktrees[:min(3, len(worktrees))] {
		names = append(names, w.Path)
	}
	summary := fmt.Sprintf("Low disk: %d bytes free (threshold %d). Driga can show known cleanup candidates with driga cleanup --sizes; Machine shows the largest apps.", s.DiskFree, threshold)
	if len(names) > 0 {
		summary += " Oldest stored worktrees to inspect: " + strings.Join(names, ", ")
	}
	if active == "1" {
		return tx.RefreshMachineWarning("low_disk", summary, machineJump())
	}
	raw, err := tx.Meta("cleanup:low_disk_episode")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	episode, _ := strconv.ParseUint(raw, 10, 64)
	episode++
	if err := tx.UpsertInboxItem(model.InboxItem{ID: fmt.Sprintf("low_disk:%d", episode), Kind: "low_disk", TS: now, Summary: summary, Jump: machineJump()}); err != nil {
		return err
	}
	if err := tx.SetMeta("cleanup:low_disk_episode", strconv.FormatUint(episode, 10)); err != nil {
		return err
	}
	return tx.SetMeta("cleanup:low_disk", "1")
}
