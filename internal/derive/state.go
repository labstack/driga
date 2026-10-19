// Package derive computes session and project state from collected observations.
package derive

import (
	"fmt"
	"strings"
	"time"

	"github.com/labstack/driga/internal/model"
)

// Thresholds controls how long a busy session can be quiet before suspicion.
type Thresholds struct {
	StuckAfter time.Duration
}

// StateInput combines adapter, process, hook, and acknowledgment observations.
type StateInput struct {
	Obs                                          model.SessionObs
	Proc                                         *model.Proc
	LastNotification, LastStop                   *time.Time
	LastUserPrompt                               time.Time
	Unseen                                       bool
	LastDecision, LastAPIRequest, LastToolResult *time.Time
	LastAPIError                                 string
}

// Display adds the display marker to a heuristic label.
func Display(label string, heuristic bool) string {
	if heuristic {
		return "~" + label
	}
	return label
}

// Label returns a state, whether it is heuristic, and human-readable evidence.
func Label(in StateInput, now time.Time, th Thresholds) (label string, heuristic bool, evidence string) {
	defer func() {
		if (label == "working" || label == "stalled?") && in.LastAPIError != "" {
			evidence += "; " + in.LastAPIError
		}
	}()
	if th.StuckAfter <= 0 {
		th.StuckAfter = 10 * time.Minute
	}
	age := now.Sub(in.Obs.LastActivityAt)
	quiet := "no log growth " + stateDuration(age)
	if in.Obs.LastActivityAt.IsZero() {
		quiet = "no activity timestamp"
	}
	switch in.Obs.Adapter {
	case "claude":
		cleared := in.LastUserPrompt
		for _, ts := range []*time.Time{in.LastDecision, in.LastToolResult} {
			if ts != nil && ts.After(cleared) {
				cleared = *ts
			}
		}
		if in.LastNotification != nil && in.LastNotification.After(in.Obs.LastActivityAt) && in.LastNotification.After(cleared) {
			return "waiting", false, "Notification newer than last activity"
		}
		finished := in.LastStop != nil && in.LastStop.After(in.LastUserPrompt) && in.Unseen
		// A dead registry must not look busy. Move its liveness check before
		// busy rules, but preserve an unseen Stop as finished even after exit.
		// Live sessions retain the table's busy-before-Stop rule order.
		if in.Obs.PID == 0 && in.Obs.RegistryStatus != "" {
			if finished {
				return "finished", false, "unacknowledged Stop newer than last user prompt; process gone"
			}
			return "dead", false, "registry present, process gone"
		}
		if in.Obs.PID != 0 && in.LastToolResult != nil && !in.LastToolResult.Before(in.Obs.LastActivityAt) && (in.LastStop == nil || in.LastToolResult.After(*in.LastStop)) && now.Sub(*in.LastToolResult) < th.StuckAfter {
			return "working", false, "OTLP activity " + stateDuration(now.Sub(*in.LastToolResult)) + " ago"
		}
		if in.Obs.PID != 0 && in.LastAPIRequest != nil && in.LastAPIRequest.After(in.Obs.LastActivityAt) && (in.LastStop == nil || in.LastAPIRequest.After(*in.LastStop)) && now.Sub(*in.LastAPIRequest) < th.StuckAfter {
			return "working", false, "API call " + stateDuration(now.Sub(*in.LastAPIRequest)) + " ago"
		}
		if in.Obs.RegistryStatus == "busy" {
			if age < th.StuckAfter {
				return "working", false, "busy, " + quiet
			}
			if in.Proc == nil {
				return "stalled?", false, "busy, " + quiet + ", no process sample"
			}
			evidence = fmt.Sprintf("busy, %s, CPU %.1f%%", quiet, in.Proc.CPUPercent)
			if in.Proc.CPUPercent >= 1 {
				return "unknown", false, evidence
			}
			return "stalled?", false, evidence
		}
		if finished {
			return "finished", false, "unacknowledged Stop newer than last user prompt"
		}
		if in.Obs.RegistryStatus == "idle" || in.Obs.RegistryStatus == "shell" {
			return "idle", false, "registry " + in.Obs.RegistryStatus
		}
		return "unknown", false, "no recognized Claude registry or hook state"
	case "codex":
		if in.Obs.Hosted {
			if age < time.Minute {
				return "working", true, "hosted session, activity " + stateDuration(age) + " ago"
			}
			if !in.Obs.TurnOpen {
				return "idle", true, "hosted session, no open turn, " + quiet
			}
			return "unknown", true, "hosted session, turn open, " + quiet + ", process liveness unavailable"
		}
		if in.Proc == nil {
			if !in.Obs.TurnOpen {
				return "finished", false, "process gone after completed turn"
			}
			return "dead", false, "process gone with turn still open"
		}
		if age < time.Minute {
			return "working", false, "process alive, activity " + stateDuration(age) + " ago"
		}
		if in.Obs.TurnOpen && age >= th.StuckAfter {
			return "stalled?", false, "turn open, " + quiet
		}
		if !in.Obs.TurnOpen {
			if in.Unseen {
				return "finished", false, "unacknowledged turn completion, process alive"
			}
			return "idle", false, "turn completed, process alive"
		}
		return "unknown", false, "turn open, " + quiet + "; no reliable approval-waiting signal"
	default:
		return "unknown", false, "unrecognized adapter"
	}
}

func stateDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d >= time.Minute {
		s := strings.TrimSuffix(d.Round(time.Minute).String(), "0s")
		if strings.HasSuffix(s, "h0m") {
			s = strings.TrimSuffix(s, "0m")
		}
		return s
	}
	return d.Round(time.Second).String()
}
