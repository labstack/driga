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
	trends := derive.NewMCPTrendBuilder()
	if err := tx.ScanMCPMinutes(now, func(s model.MCPMinute) error { trends.Add(s); return nil }); err != nil {
		return err
	}
	// Sessions are read only once the alert is due, never on every pass.
	var sessions []model.Session
	if len(processSamples) > 0 {
		if sessions, err = tx.Sessions(store.SessionFilter{}); err != nil {
			return err
		}
	}
	summary := memoryPressureSummaryWithMemory(processSamples, sessions, now, trends.Trends(), s.MemUsed)
	active, err := tx.Meta("machine:pressure_active")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if active == "1" {
		return tx.RefreshMachineWarning("memory_pressure", summary, machineJump())
	}
	raw, err = tx.Meta("machine:pressure_episode")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	episode, _ := strconv.ParseUint(raw, 10, 64)
	episode++
	item := model.InboxItem{ID: fmt.Sprintf("memory_pressure:%d", episode), Kind: "memory_pressure", TS: now, Summary: summary, Jump: machineJump()}
	if err := tx.UpsertInboxItem(item); err != nil {
		return err
	}
	if err := tx.SetMeta("machine:pressure_episode", strconv.FormatUint(episode, 10)); err != nil {
		return err
	}
	return tx.SetMeta("machine:pressure_active", "1")
}

// memoryPressureSummary uses the sampled process total when no OS reading is
// available. The inbox supplies the actual machine memory in use below.
func memoryPressureSummary(samples [][]model.Proc, sessions []model.Session, now time.Time, trends []derive.MCPTrend) string {
	var total uint64
	if len(samples) > 0 {
		for _, p := range samples[0] {
			if !p.Zombie {
				memory, _ := derive.ProcessMemory(p)
				total += memory
			}
		}
	}
	return memoryPressureSummaryWithMemory(samples, sessions, now, trends, total)
}
func memoryPressureSummaryWithMemory(samples [][]model.Proc, sessions []model.Session, now time.Time, trends []derive.MCPTrend, machineMemory uint64) string {
	var memory uint64
	servers := derive.MCPServerView{}
	if len(samples) > 0 {
		servers = derive.MCPServers(samples[0], nil, now)
	}
	if len(samples) > 0 {
		for _, p := range derive.PlaceProcesses(samples[0], sessions, nil) {
			if p.Bucket == derive.BucketSession || p.Bucket == derive.BucketAgent {
				memory += p.Memory
			}
		}
	}
	summary := "Critical memory pressure for two samples. "
	if memory == 0 {
		summary += fmt.Sprintf("Agents hold none of the %.1f GiB in use; other apps are the cause.", float64(machineMemory)/(1<<30))
	} else {
		count := 0
		for _, c := range servers.Commands {
			count += c.Count
		}
		pct := float64(0)
		if machineMemory > 0 {
			pct = 100 * float64(memory) / float64(machineMemory)
		}
		summary += fmt.Sprintf("Agents hold %.1f GiB of %.1f GiB in use (%.0f%%), %d MCP servers.", float64(memory)/(1<<30), float64(machineMemory)/(1<<30), pct, count)
	}
	if len(samples) > 0 {
		derive.ApplyMCPTrends(&servers, trends)
		if len(servers.Pileups) > 0 {
			p := servers.Pileups[0]
			summary += fmt.Sprintf(" MCP pile-ups: %d groups; largest %s (%d processes, %.1f GiB). %s", len(servers.Pileups), p.Name, p.Count, float64(p.Memory)/(1<<30), p.Advice)
		}
		var largest *derive.MCPTrend
		for _, c := range servers.Commands {
			for _, t := range c.Trends {
				if t.State == "leaking" && (largest == nil || t.Memory > largest.Memory) {
					largest = &t
				}
			}
		}
		if largest != nil {
			t := *largest
			interval := 60 / t.RatePerHour
			cadence := fmt.Sprintf("%.0f min", interval)
			if interval < 1 {
				cadence = fmt.Sprintf("%.0f sec", interval*60)
			}
			summary += fmt.Sprintf(" Largest observed growth: %s PID %d adds about 1 %s every %s, none closed since %s (observed through %s; window starts %s; %.2g/h). Restart the named host app/session.", t.Host.Name, t.Host.PID, t.Name, cadence, t.FirstSample.In(time.Local).Format("15:04"), t.LastSample.Format(time.RFC3339), t.FirstSample.Format(time.RFC3339), t.RatePerHour)
		}
	}
	return summary
}
