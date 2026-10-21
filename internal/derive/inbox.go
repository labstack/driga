package derive

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/labstack/driga/internal/adapter/copilot"
	"github.com/labstack/driga/internal/model"
)

// InboxItems returns deduplicated signal facts. Old completions use the import
// baseline; a waiting signal never becomes an acknowledgement just by being read.
// Worker completions group on the lead; waits retain their own session identity.
// Codex notify hooks are deliberately ignored: rollouts already emit completions.
func InboxItems(sessions []model.Session, parents map[int64]int64, hooks []model.HookEvent, baseline time.Time) []model.InboxItem {
	var result []model.InboxItem
	for _, item := range inboxEvents(sessions, hooks, baseline) {
		root := inboxRoot(item.SessionID, parents)
		if root != item.SessionID && item.Kind == "finished" {
			item.Kind = "worker_finished"
		}
		if item.Kind != "waiting" {
			item.SessionID = root
		}
		result = append(result, item)
	}
	return result
}

// FinishedChildCounts counts each worker completion newer than baseline on
// its top-level parent. It uses the same event IDs and deduplication as inbox.
func FinishedChildCounts(sessions []model.Session, parents map[int64]int64, hooks []model.HookEvent, baseline time.Time) map[int64]int {
	counts := make(map[int64]int)
	for _, item := range inboxEvents(sessions, hooks, baseline) {
		root := inboxRoot(item.SessionID, parents)
		if root != item.SessionID && item.Kind == "finished" && item.AckedAt == nil {
			counts[root]++
		}
	}
	return counts
}

func inboxEvents(sessions []model.Session, hooks []model.HookEvent, baseline time.Time) []model.InboxItem {
	type sessionKey struct{ adapter, external string }
	byKey := make(map[sessionKey]int64)
	var items []model.InboxItem
	add := func(adapter, external, suffix string, session int64, kind string, ts time.Time, text string) {
		if strings.TrimSpace(text) == "" {
			if kind == "waiting" {
				text = "waiting for input"
			} else {
				text = "turn finished"
			}
		}
		id := adapter + ":" + external + ":"
		if !strings.HasPrefix(suffix, id) {
			id += suffix
		} else {
			id = suffix
		}
		item := model.InboxItem{ID: id, SessionID: session, Kind: kind, TS: ts, Summary: text}
		if kind == "waiting" {
			item.Reason = "unknown"
			item.Source = "hook"
			item.WaitingSinceAt = ts
		}
		if kind != "waiting" && !ts.After(baseline) {
			at := baseline
			item.AckedAt = &at
		}
		items = append(items, item)
	}
	for _, s := range sessions {
		key := sessionKey{s.Adapter, s.ExternalID}
		if old, ok := byKey[key]; !ok || s.ID < old {
			byKey[key] = s.ID
		}
		for _, completion := range s.Completions {
			add(s.Adapter, s.ExternalID, completion.ID, s.ID, "finished", completion.TS, completion.Text)
		}
	}
	for _, h := range hooks {
		if h.Adapter == "codex" && h.Event == "notify" {
			continue
		}
		id, ok := byKey[sessionKey{h.Adapter, h.SessionID}]
		if !ok {
			continue
		}
		var kind string
		if h.Adapter == "copilot" {
			if h.Event == "agentStop" {
				kind = "finished"
			} else if copilot.Wait(h) {
				kind = "waiting"
			} else {
				continue
			}
		}
		if kind == "" {
			switch h.Event {
			case "Stop":
				kind = "finished"
			case "Notification":
				if h.NotificationType == "idle_prompt" {
					continue
				}
				kind = "waiting"
			case "PermissionRequest":
				kind = "waiting"
			default:
				continue
			}
		}
		add(h.Adapter, h.SessionID, fmt.Sprint(h.TS.UnixNano()), id, kind, h.TS, h.Message)
		if kind == "waiting" {
			i := &items[len(items)-1]
			i.Reason = NotificationReason(h.NotificationType)
			if h.Event == "PermissionRequest" {
				i.Reason = "permission"
			}
			i.Tool, i.Detail = h.Tool, h.Detail
		}
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if !a.TS.Equal(b.TS) {
			return a.TS.Before(b.TS)
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		if a.SessionID != b.SessionID {
			return a.SessionID < b.SessionID
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Summary < b.Summary
	})
	seen := make(map[string]bool)
	var result []model.InboxItem
	for _, item := range items {
		if !seen[item.ID] {
			result = append(result, item)
			seen[item.ID] = true
		}
	}
	return result
}

// NotificationReason maps protocol metadata, never message text, to a reason.
func NotificationReason(typ string) string {
	switch typ {
	case "permission_prompt":
		return "permission"
	case "agent_needs_input":
		return "question"
	case "elicitation_dialog", "elicitation_url_dialog":
		return "elicitation"
	default:
		return "unknown"
	}
}

func inboxRoot(id int64, parents map[int64]int64) int64 {
	seen := make(map[int64]bool)
	for {
		if seen[id] {
			root := id
			for p := parents[id]; p != id; p = parents[p] {
				if p < root {
					root = p
				}
			}
			return root
		}
		seen[id] = true
		parent, ok := parents[id]
		if !ok || parent == 0 {
			return id
		}
		id = parent
	}
}
