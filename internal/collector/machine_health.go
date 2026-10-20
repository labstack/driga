package collector

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func machineJump() *model.InboxJump {
	return &model.InboxJump{Kind: "machine", Command: "driga top --tab machine"}
}

// Only successful, distinct OS readings count. Unknown readings and source
// failures break the streak; the durable counter also works after a restart.
func memoryPressureInbox(tx *store.Tx, s model.MachineSample, now time.Time, processSamples ...[]model.Proc) error {
	if !s.TS.IsZero() {
		raw, err := tx.Meta("machine:pressure_sample_at")
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if previous, err := time.Parse(time.RFC3339Nano, raw); err == nil && !s.TS.After(previous) {
			return nil
		}
		if err := tx.SetMeta("machine:pressure_sample_at", s.TS.Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	if s.PressureLevel != "critical" {
		if err := tx.SetMeta("machine:critical_count", "0"); err != nil {
			return err
		}
		if err := tx.SetMeta("machine:pressure_active", "0"); err != nil {
			return err
		}
		return tx.EndMachineWarning("memory_pressure", now)
	}
	raw, err := tx.Meta("machine:critical_count")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	count, _ := strconv.Atoi(raw)
	if count < 2 {
		count++
	}
	if err := tx.SetMeta("machine:critical_count", strconv.Itoa(count)); err != nil {
		return err
	}
	if count < 2 {
		return nil
	}
	active, err := tx.Meta("machine:pressure_active")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if active == "1" {
		return tx.RefreshMachineWarning("memory_pressure", memoryPressureSummary(processSamples, now), machineJump())
	}
	raw, err = tx.Meta("machine:pressure_episode")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	episode, _ := strconv.ParseUint(raw, 10, 64)
	episode++
	item := model.InboxItem{ID: fmt.Sprintf("memory_pressure:%d", episode), Kind: "memory_pressure", TS: now, Summary: memoryPressureSummary(processSamples, now), Jump: machineJump()}
	if err := tx.UpsertInboxItem(item); err != nil {
		return err
	}
	if err := tx.SetMeta("machine:pressure_episode", strconv.FormatUint(episode, 10)); err != nil {
		return err
	}
	return tx.SetMeta("machine:pressure_active", "1")
}

func memoryPressureSummary(samples [][]model.Proc, now time.Time) string {
	summary := "Critical memory pressure for two samples. Driga can show the largest apps by memory and CPU in Machine."
	if len(samples) > 0 {
		servers := derive.MCPServers(samples[0], nil, now)
		if len(servers.Pileups) > 0 {
			p := servers.Pileups[0]
			summary += fmt.Sprintf(" MCP pile-ups: %d groups; largest %s (%d processes, %.1f GiB). %s", len(servers.Pileups), p.Name, p.Count, float64(p.Memory)/(1<<30), p.Advice)
		}
	}
	return summary
}
