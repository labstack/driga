// Package copilot folds documented hook observations. It never reads session-state files.
package copilot

import (
	"path/filepath"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/scrub"
)

// Event reports whether an event belongs to the report-only integration.
func Event(event string) bool {
	switch event {
	case "sessionStart", "userPromptSubmitted", "postToolUse", "postToolUseFailure", "notification", "agentStop", "subagentStart", "subagentStop", "sessionEnd":
		return true
	}
	return false
}

// Wait reports only notifications that actually ask a person for input.
func Wait(h model.HookEvent) bool {
	return h.Event == "notification" && (h.NotificationType == "permission_prompt" || h.NotificationType == "elicitation_dialog")
}

// Recent bounds hook-only liveness like hosted sessions. An initial wait has no
// activity timestamp, so use the session's first observation without treating
// the notification as activity.
func Recent(s model.SessionObs, now time.Time) bool {
	at := s.LastActivityAt
	if at.IsZero() {
		at = s.StartedAt
	}
	return !at.IsZero() && now.Sub(at) <= 24*time.Hour
}

// Fold applies one observation to the session's persisted fold. Delayed events
// cannot roll the state backwards; prompts and activity remain session scoped.
func Fold(s *model.SessionObs, h model.HookEvent) {
	if !Event(h.Event) {
		return
	}
	if s.ExternalID == "" {
		s.Adapter = "copilot"
		s.ExternalID = h.SessionID
		s.Origin = "hook"
		s.StartedAt = h.TS
	}
	if h.TS.Before(s.LastActivityAt) {
		return
	}
	if h.Cwd != "" {
		s.Cwd = h.Cwd
	}
	if h.PID != 0 && !h.ProcStart.IsZero() {
		s.PID = h.PID
		s.ProcStart = h.ProcStart
	}
	if h.Origin == "vscode" {
		s.Origin, s.PID, s.ProcStart = "vscode", 0, time.Time{}
	}
	s.Hosted = s.PID == 0 && s.ProcStart.IsZero()
	if h.Event != "notification" {
		s.LastActivityAt = h.TS
	}
	switch h.Event {
	case "sessionStart":
		s.RegistryStatus = "started"
		s.TurnOpen = false
		if h.Source == "resume" && s.Origin != "vscode" {
			s.Origin = "resume"
		}
	case "userPromptSubmitted":
		if s.LastPromptAt.IsZero() && h.Title != "" {
			s.Title = scrub.Snippet(h.Title)
		}
		s.RegistryStatus = "busy"
		s.TurnOpen = true
		s.LastPromptAt = h.TS
		s.LastPrompt = scrub.Snippet(h.Prompt)
	case "postToolUse", "postToolUseFailure":
		s.RegistryStatus = "busy"
		s.TurnOpen = true
	case "notification":
		if Wait(h) {
			s.RegistryStatus = "waiting"
		}
	case "agentStop":
		s.RegistryStatus = "idle"
		s.TurnOpen = false
	case "sessionEnd":
		s.RegistryStatus = "ended"
		s.TurnOpen = false
	}
	if s.Title == "" && s.Cwd != "" {
		s.Title = filepath.Base(s.Cwd)
	}
}
