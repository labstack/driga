// Package derive computes session and project state from collected observations.
package derive

import (
	"fmt"
	"strings"
	"time"

	"github.com/labstack/driga/internal/adapter/copilot"
	"github.com/labstack/driga/internal/model"
)

// Thresholds controls how long a busy session can be quiet before suspicion.
type Thresholds struct {
	StuckAfter time.Duration
}

// StateInput combines adapter, process, hook, and acknowledgment observations.
type StateInput = model.StateInput

// Display adds the display marker to a heuristic label.
func Display(label string, heuristic bool) string {
	if heuristic {
		return "~" + label
	}
	return label
}

// Label returns a state, whether it is heuristic, and human-readable evidence.
func Label(in StateInput, now time.Time, th Thresholds) (string, bool, string) {
	m := LabelWithEvidence(in, now, th)
	return m.Verdict, m.Heuristic, m.Evidence
}

func label(in StateInput, now time.Time, th Thresholds) (label string, heuristic bool, evidence string) {
	defer func() {
		if (label == "working" || label == "stuck") && in.LastAPIError != "" {
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
	// A registry replacement is explicit conversation termination, even if its
	// last transcript or hook still describes an open turn.
	if in.Obs.Adapter == "claude" && in.Obs.RegistryStatus == "ended" {
		return "ended", false, "conversation replaced in live process"
	}
	gone := !in.Obs.Hosted && in.Proc == nil
	if in.Obs.Adapter == "claude" {
		// A transcript without a registry cannot prove Claude process loss.
		gone = !in.Obs.Hosted && in.Obs.PID == 0 && in.Obs.RegistryStatus != ""
	}
	// Explicit lifecycle hooks supersede older transcript/registry observations.
	// Newer activity and telemetry still win over delayed hook delivery.
	if in.Obs.Adapter == "claude" || in.Obs.Adapter == "codex" {
		latest := in.Obs.LastActivityAt
		for _, ts := range []time.Time{in.Obs.RegistryUpdatedAt, in.LastUserPrompt} {
			if ts.After(latest) {
				latest = ts
			}
		}
		for _, ts := range []*time.Time{in.LastNotification, in.LastDecision, in.LastToolResult, in.LastAPIRequest} {
			if ts != nil && ts.After(latest) {
				latest = *ts
			}
		}
		if !in.LastLifecycleAt.IsZero() && !in.LastLifecycleAt.Before(latest) {
			switch in.LastLifecycle {
			case "SessionEnd":
				return "ended", false, "hook SessionEnd"
			case "SessionStart", "Notification:idle_prompt":
				if gone {
					break
				}
				return "idle", false, "hook " + in.LastLifecycle
			case "UserPromptSubmit":
				if gone {
					break
				}
				if now.Sub(in.LastLifecycleAt) >= th.StuckAfter {
					return "stuck", false, "hook turn open, no log growth " + stateDuration(now.Sub(in.LastLifecycleAt))
				}
				return "working", false, "hook UserPromptSubmit"
			case "Stop":
				if gone {
					return "ended", false, "process gone after hook Stop"
				}
				if in.Unseen {
					return "finished", false, "hook unacknowledged Stop"
				}
				return "idle", false, "hook Stop acknowledged"
			}
		}
		// Codex's PermissionRequest is explicit evidence; rollouts alone cannot
		// infer approval waits. Keep liveness checks for unhosted, exited sessions.
		if in.Obs.Adapter == "codex" && (in.Proc != nil || in.Obs.Hosted) && in.LastNotification != nil {
			cleared := in.Obs.LastActivityAt
			for _, ts := range []time.Time{in.LastUserPrompt, in.LastLifecycleAt} {
				if ts.After(cleared) {
					cleared = ts
				}
			}
			for _, ts := range []*time.Time{in.LastDecision, in.LastToolResult} {
				if ts != nil && ts.After(cleared) {
					cleared = *ts
				}
			}
			if in.LastNotification.After(cleared) {
				return "waiting", false, "hook PermissionRequest newer than activity"
			}
		}
	}
	switch in.Obs.Adapter {
	case "copilot":
		live := in.Obs.PID != 0 || in.Obs.Hosted && copilot.Recent(in.Obs, now)
		switch in.Obs.RegistryStatus {
		case "ended":
			return "ended", false, "Copilot sessionEnd"
		case "error":
			if live {
				return "finished", false, "Copilot errorOccurred"
			}
			return "dead", false, "Copilot fatal errorOccurred"
		case "error_recoverable":
			if !live {
				return "ended", false, "Copilot process gone after recoverable error"
			}
			return "finished", false, "Copilot recoverable errorOccurred"
		case "started":
			if live {
				return "idle", false, "Copilot sessionStart"
			}
		case "waiting":
			if live {
				return "waiting", false, "Copilot notification requests input"
			}
		case "busy":
			if live {
				if age >= th.StuckAfter {
					return "stuck", false, "Copilot turn open, " + quiet
				}
				return "working", false, "Copilot turn activity"
			}
		case "idle":
			if !live {
				return "ended", false, "Copilot process gone after completed turn"
			}
			if in.Unseen {
				return "finished", false, "unacknowledged Copilot agentStop"
			}
			if live {
				return "idle", false, "Copilot session started or turn completed"
			}
		}
		if in.Obs.RegistryStatus == "" && in.Proc != nil {
			return "unknown", false, "Copilot process alive; install hooks with driga hooks install copilot"
		}
		if in.Proc == nil {
			if in.Obs.TurnOpen || in.Obs.RegistryStatus == "busy" || in.Obs.RegistryStatus == "waiting" {
				return "dead", false, "Copilot process gone with turn still open"
			}
			if in.Obs.RegistryStatus != "" {
				return "ended", false, "Copilot process gone with no open turn"
			}
			return "unknown", false, "Copilot identity unavailable; no lifecycle evidence"
		}
		return "unknown", false, "no recognized Copilot hook state"
	case "claude":
		cleared := in.LastUserPrompt
		if in.LastLifecycleAt.After(cleared) {
			cleared = in.LastLifecycleAt
		}
		for _, ts := range []*time.Time{in.LastDecision, in.LastToolResult, in.LastStop} {
			if ts != nil && ts.After(cleared) {
				cleared = *ts
			}
		}
		finished := in.LastStop != nil && in.LastStop.After(in.LastUserPrompt) && !in.LastStop.Before(in.LastLifecycleAt) && in.Unseen
		// Liveness precedes busy/waiting and unread-completion display rules.
		// A completed turn stays in the inbox independently of session state.
		if gone {
			if claudeOpenTurn(in) {
				return "dead", false, "process gone with turn still open"
			}
			return "ended", false, "process gone with no open turn"
		}
		if in.LastNotification != nil && in.LastNotification.After(in.Obs.LastActivityAt) && in.LastNotification.After(cleared) {
			return "waiting", false, "Notification newer than last activity"
		}
		if in.Obs.PID != 0 && in.LastToolResult != nil && !in.LastToolResult.Before(in.Obs.LastActivityAt) && (in.LastStop == nil || in.LastToolResult.After(*in.LastStop)) && now.Sub(*in.LastToolResult) < th.StuckAfter {
			return "working", false, "OTLP activity " + stateDuration(now.Sub(*in.LastToolResult)) + " ago"
		}
		if in.Obs.PID != 0 && in.LastAPIRequest != nil && in.LastAPIRequest.After(in.Obs.LastActivityAt) && (in.LastStop == nil || in.LastAPIRequest.After(*in.LastStop)) && now.Sub(*in.LastAPIRequest) < th.StuckAfter {
			return "working", false, "API call " + stateDuration(now.Sub(*in.LastAPIRequest)) + " ago"
		}
		if in.Obs.RegistryStatus == "busy" {
			// A claimed background conversation may be busy before a transcript
			// exists. The registry transition is also positive activity evidence.
			busyAge := now.Sub(claudeBusyActivity(in.Obs))
			if in.Obs.RegistryUpdatedAt.After(in.Obs.LastActivityAt) {
				quiet = "registry updated " + stateDuration(busyAge) + " ago"
			}
			if busyAge < th.StuckAfter {
				return "working", false, "busy, " + quiet
			}
			if in.Proc == nil {
				return "stuck", false, "busy, " + quiet + ", no process sample"
			}
			evidence = fmt.Sprintf("busy, %s, CPU %.1f%%", quiet, in.Proc.CPUPercent)
			if in.Proc.CPUPercent >= 1 {
				return "unknown", false, evidence
			}
			return "stuck", false, evidence
		}
		if in.Obs.PID != 0 && (in.Obs.RegistryStatus == "idle" || in.Obs.RegistryStatus == "shell") && in.LastStop != nil && !in.Obs.RegistryUpdatedAt.Before(*in.LastStop) {
			return "idle", false, "registry " + in.Obs.RegistryStatus + " newer than Stop"
		}
		if finished {
			return "finished", false, "unacknowledged Stop newer than last user prompt"
		}
		if in.Obs.RegistryStatus == "idle" || in.Obs.RegistryStatus == "shell" {
			return "idle", false, "registry " + in.Obs.RegistryStatus
		}
		return "unknown", false, "no recognized Claude registry or hook state"
	case "agy":
		if in.Proc == nil {
			return "unknown", false, "agy process gone; unsupported-version: end reason unavailable"
		}
		return "unknown", false, "agy process alive; unsupported-version: title/state schema not yet supported"
	case "codex", "gemini":
		if in.Proc != nil && in.Obs.LastActivityAt.IsZero() {
			return "unknown", false, "process alive; no session activity observed"
		}
		// Gemini follows Codex without hooks: an open tool call may still be
		// executing, so neither ever infers a permission wait.
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
				return "ended", false, "process gone after completed turn"
			}
			return "dead", false, "process gone with turn still open"
		}
		if age < time.Minute {
			return "working", false, "process alive, activity " + stateDuration(age) + " ago"
		}
		if in.Obs.TurnOpen && age >= th.StuckAfter {
			return "stuck", false, "turn open, " + quiet
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

func claudeBusyActivity(o model.SessionObs) time.Time {
	if o.RegistryUpdatedAt.After(o.LastActivityAt) {
		return o.RegistryUpdatedAt
	}
	return o.LastActivityAt
}

// claudeOpenTurn compares positive work signals with explicit turn closures.
// A newer idle registry or Stop closes older transcript/telemetry activity;
// a prompt or tool/API call after that closure opens the next turn.
func claudeOpenTurn(in StateInput) bool {
	open := in.Obs.TurnOpen || in.Obs.RegistryStatus == "busy"
	openedAt := in.LastUserPrompt
	if in.Obs.TurnOpen && in.Obs.LastActivityAt.After(openedAt) {
		openedAt = in.Obs.LastActivityAt
	}
	for _, ts := range []*time.Time{in.LastToolResult, in.LastAPIRequest} {
		if ts != nil && ts.After(openedAt) {
			openedAt = *ts
		}
	}
	if in.LastLifecycle == "UserPromptSubmit" && in.LastLifecycleAt.After(openedAt) {
		openedAt = in.LastLifecycleAt
	}
	closedAt := time.Time{}
	if in.Obs.RegistryStatus == "idle" || in.Obs.RegistryStatus == "shell" {
		closedAt = in.Obs.RegistryUpdatedAt
	}
	if in.LastStop != nil && in.LastStop.After(closedAt) {
		closedAt = *in.LastStop
	}
	switch in.LastLifecycle {
	case "Stop", "SessionStart", "Notification:idle_prompt":
		if in.LastLifecycleAt.After(closedAt) {
			closedAt = in.LastLifecycleAt
		}
	}
	if !openedAt.IsZero() && openedAt.After(closedAt) {
		open = true
	}
	if !closedAt.IsZero() && !closedAt.Before(openedAt) && !closedAt.Before(in.Obs.RegistryUpdatedAt) {
		open = false
	}
	return open
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
