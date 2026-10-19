package derive

import (
	"fmt"
	"math"
	"time"

	"github.com/labstack/driga/internal/model"
)

// LabelWithEvidence runs the state table once and explains its four inputs.
// Rows may disagree: a recent Stop does not override newer busy/tool activity.
func LabelWithEvidence(in StateInput, now time.Time, th Thresholds) model.StateMixer {
	verdict, heuristic, evidence := label(in, now, th)
	m := model.StateMixer{Verdict: verdict, Heuristic: heuristic, Evidence: evidence, ObservedAt: now}
	if th.StalledAfter <= 0 {
		th.StalledAfter = 10 * time.Minute
	}
	add := func(name, reading string, at time.Time, available bool, state string) {
		row := model.StateSignal{Name: name, Reading: reading, At: at}
		if available {
			row.Strength = 1
			if !at.IsZero() {
				row.Age = max(time.Duration(0), now.Sub(at))
				row.Strength = 1 / (1 + float64(row.Age)/float64(th.StalledAfter))
			}
			row.Supports = state == verdict
		}
		if row.Supports {
			m.Agreement++
		}
		m.Rows = append(m.Rows, row)
	}
	registryState := "unknown"
	switch in.Obs.RegistryStatus {
	case "busy":
		registryState = "working"
		if now.Sub(in.Obs.LastActivityAt) >= th.StalledAfter {
			registryState = "stalled"
			if in.Proc != nil && in.Proc.CPUPercent >= 1 {
				registryState = "unknown"
			}
		}
	case "idle", "shell":
		registryState = "idle"
	}
	if in.Obs.PID == 0 && in.Obs.RegistryStatus != "" {
		registryState = "dead"
	}
	add("registry", in.Obs.RegistryStatus, in.Obs.LastActivityAt, in.Obs.RegistryStatus != "", registryState)
	hookAt := time.Time{}
	hookName := "unavailable"
	hookState := "unknown"
	for _, v := range []struct {
		name, state string
		at          *time.Time
	}{{"Stop", "finished", in.LastStop}, {"Notification", "waiting", in.LastNotification}} {
		if v.at != nil && v.at.After(hookAt) {
			hookAt = *v.at
			hookName = v.name
			hookState = v.state
		}
	}
	if hookState == "finished" && !in.Unseen {
		hookState = "idle"
	}
	if in.LastHookAt.After(hookAt) {
		hookAt = in.LastHookAt
		hookName = in.LastHook
		hookState = "unknown"
	}
	add("hooks", hookName, hookAt, !hookAt.IsZero(), hookState)
	telemetryAt := time.Time{}
	telemetryName := "unavailable"
	for _, v := range []struct {
		name string
		at   *time.Time
	}{{"decision", in.LastDecision}, {"api_request", in.LastAPIRequest}, {"tool_result", in.LastToolResult}} {
		if v.at != nil && v.at.After(telemetryAt) {
			telemetryAt = *v.at
			telemetryName = v.name
		}
	}
	ts := "unknown"
	if !telemetryAt.IsZero() && now.Sub(telemetryAt) < th.StalledAfter {
		ts = "working"
	}
	add("telemetry", telemetryName, telemetryAt, !telemetryAt.IsZero(), ts)
	alive := in.Proc != nil
	ps := "unknown"
	reading := "unavailable"
	available := !in.Obs.Hosted && (in.Obs.PID != 0 || in.Obs.RegistryStatus != "")
	// Claude can retain a live registry without a successful process sample.
	// A missing sample is not proof of process death.
	if in.Obs.Adapter == "claude" && in.Obs.PID != 0 && in.Proc == nil {
		available = false
	}
	if available {
		reading = "process gone"
		ps = "dead"
		if alive {
			reading = fmt.Sprintf("alive · CPU %.1f%%", in.Proc.CPUPercent)
			ps = "idle"
			if now.Sub(in.Obs.LastActivityAt) < time.Minute {
				ps = "working"
			}
			if in.Obs.TurnOpen && now.Sub(in.Obs.LastActivityAt) >= th.StalledAfter {
				ps = "stalled"
			}
			if in.Obs.RegistryStatus == "busy" && now.Sub(in.Obs.LastActivityAt) >= th.StalledAfter {
				ps = "stalled"
				if in.Proc.CPUPercent >= 1 {
					ps = "unknown"
				}
			}
		} else if !in.Obs.TurnOpen && (in.Obs.Adapter == "codex" || in.Obs.Adapter == "gemini") {
			ps = "finished"
		}
	}
	add("process", reading, time.Time{}, available, ps)
	if available {
		m.Rows[3].Alive = &alive
	}
	if alive {
		cpu := in.Proc.CPUPercent
		if !math.IsNaN(cpu) && !math.IsInf(cpu, 0) {
			m.Rows[3].CPUPercent = &cpu
		}
	}
	return m
}
