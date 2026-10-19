package derive

import (
	"fmt"
	"math"
	"strings"
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
	add := func(name, reading string, at time.Time, available bool) {
		row := model.StateSignal{Name: name, Reading: reading, At: at}
		if available {
			row.Strength = 1
			if !at.IsZero() {
				row.Age = max(time.Duration(0), now.Sub(at))
				row.Strength = 1 / (1 + float64(row.Age)/float64(th.StalledAfter))
			}
		}
		m.Rows = append(m.Rows, row)
	}
	add("registry", in.Obs.RegistryStatus, in.Obs.LastActivityAt, in.Obs.RegistryStatus != "")
	hookAt := time.Time{}
	hookName := "unavailable"
	for _, v := range []struct {
		name string
		at   *time.Time
	}{{"Stop", in.LastStop}, {"Notification", in.LastNotification}} {
		if v.at != nil && v.at.After(hookAt) {
			hookAt = *v.at
			hookName = v.name
		}
	}
	if in.LastHookAt.After(hookAt) {
		hookAt = in.LastHookAt
		hookName = in.LastHook
	}
	add("hooks", hookName, hookAt, !hookAt.IsZero())
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
	add("telemetry", telemetryName, telemetryAt, !telemetryAt.IsZero())
	alive := in.Proc != nil
	reading := "unavailable"
	available := !in.Obs.Hosted && (in.Obs.PID != 0 || in.Obs.RegistryStatus != "" || in.Obs.Adapter == "agy" || in.Obs.Adapter == "codex" || in.Obs.Adapter == "gemini")
	// A missing Claude process sample is not proof of death.
	if in.Obs.Adapter == "claude" && in.Obs.PID != 0 && !alive {
		available = false
	}
	if available {
		reading = "process gone"
		if alive {
			reading = fmt.Sprintf("alive · CPU %.1f%%", in.Proc.CPUPercent)
		}
		if in.Obs.Adapter == "codex" || in.Obs.Adapter == "gemini" {
			if in.Obs.TurnOpen {
				reading += " · turn open"
			} else {
				reading += " · turn completed"
			}
		}
	}
	add("process", reading, time.Time{}, available)
	if available {
		m.Rows[3].Alive = &alive
	}
	if alive {
		cpu := in.Proc.CPUPercent
		if !math.IsNaN(cpu) && !math.IsInf(cpu, 0) {
			m.Rows[3].CPUPercent = &cpu
		}
	}
	// Support is verdict-specific, not a separate prediction from each source.
	// In particular, a live process is compatible with an unacknowledged completion.
	age := now.Sub(in.Obs.LastActivityAt)
	finished := in.LastStop != nil && in.LastStop.After(in.LastUserPrompt) && in.Unseen
	support := [4]bool{}
	deciding := "registry"
	switch in.Obs.Adapter {
	case "claude":
		switch verdict {
		case "waiting":
			support[1] = true // label checked Notification against activity and cleared waits.
			deciding = "hooks"
		case "finished":
			support[0] = in.Obs.RegistryStatus == "idle" || in.Obs.RegistryStatus == "shell"
			support[1] = finished
			support[3] = available && finished
			deciding = "hooks"
		case "idle":
			support[0] = in.Obs.RegistryStatus == "idle" || in.Obs.RegistryStatus == "shell"
			support[1] = in.LastStop != nil && in.LastStop.After(in.LastUserPrompt) && !in.Unseen
			support[3] = available && alive
		case "working":
			support[0] = in.Obs.RegistryStatus == "busy" && age < th.StalledAfter
			tool := in.LastToolResult != nil && !in.LastToolResult.Before(in.Obs.LastActivityAt) && (in.LastStop == nil || in.LastToolResult.After(*in.LastStop)) && now.Sub(*in.LastToolResult) < th.StalledAfter
			api := in.LastAPIRequest != nil && in.LastAPIRequest.After(in.Obs.LastActivityAt) && (in.LastStop == nil || in.LastAPIRequest.After(*in.LastStop)) && now.Sub(*in.LastAPIRequest) < th.StalledAfter
			support[2] = in.Obs.PID != 0 && (tool || api)
			if strings.HasPrefix(evidence, "OTLP") || strings.HasPrefix(evidence, "API") {
				deciding = "telemetry"
			}
		case "stalled":
			support[0] = in.Obs.RegistryStatus == "busy" && age >= th.StalledAfter
			support[3] = available && alive && in.Proc.CPUPercent < 1
		case "dead":
			support[0] = in.Obs.PID == 0 && in.Obs.RegistryStatus != ""
			support[3] = available && !alive
		case "unknown":
			// Missing/unrecognized registry state is negative evidence for unknown.
			support[0] = true
			support[3] = available && alive && in.Obs.RegistryStatus == "busy" && age >= th.StalledAfter && in.Proc.CPUPercent >= 1
		}
	case "codex", "gemini":
		deciding = "process"
		if in.Obs.Hosted {
			deciding = "registry"
			m.Rows[0].Reading = "hosted transcript · " + verdict
			support[0] = true
		} else {
			switch verdict {
			case "finished", "idle":
				support[3] = !in.Obs.TurnOpen
			case "dead":
				support[3] = !alive && in.Obs.TurnOpen
			case "working":
				support[3] = alive && age < time.Minute
			case "stalled":
				support[3] = alive && in.Obs.TurnOpen && age >= th.StalledAfter
			case "unknown":
				support[3] = alive && in.Obs.TurnOpen && age >= time.Minute && age < th.StalledAfter
			}
		}
	case "agy":
		deciding = "process"
		support[3] = true
	default:
		support[0] = true // The adapter itself is unrecognized.
	}
	for i := range m.Rows {
		m.Rows[i].Supports = support[i]
		if support[i] {
			m.Agreement++
		}
	}
	m.Evidence = "because " + deciding + ": " + evidence
	return m
}
