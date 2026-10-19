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
func lowDiskInbox(tx *store.Tx, cfg config.CleanupConfig, s model.MachineSample, now time.Time) error {
	if s.DiskTotal == 0 {
		return nil
	}
	threshold := min(cfg.LowDiskBytes, uint64(float64(s.DiskTotal)*cfg.LowDiskPercent/100))
	active, err := tx.Meta("cleanup:low_disk")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if s.DiskFree > threshold {
		return tx.SetMeta("cleanup:low_disk", "0")
	}
	if s.DiskFree >= threshold || active == "1" {
		return nil
	}
	raw, err := tx.Meta("cleanup:low_disk_episode")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	episode, _ := strconv.ParseUint(raw, 10, 64)
	episode++
	worktrees, err := tx.Worktrees(0)
	if err != nil {
		return err
	}
	sort.SliceStable(worktrees, func(i, j int) bool { return worktrees[i].LastCommitAt.Before(worktrees[j].LastCommitAt) })
	names := []string{}
	for _, w := range worktrees[:min(3, len(worktrees))] {
		names = append(names, w.Path)
	}
	summary := fmt.Sprintf("Low disk: %d bytes free (threshold %d). Run driga cleanup --sizes to inspect candidates.", s.DiskFree, threshold)
	if len(names) > 0 {
		summary += " Oldest stored worktrees to inspect: " + strings.Join(names, ", ")
	}
	if err := tx.UpsertInboxItem(model.InboxItem{ID: fmt.Sprintf("low_disk:%d", episode), Kind: "low_disk", TS: now, Summary: summary}); err != nil {
		return err
	}
	if err := tx.SetMeta("cleanup:low_disk_episode", strconv.FormatUint(episode, 10)); err != nil {
		return err
	}
	return tx.SetMeta("cleanup:low_disk", "1")
}
