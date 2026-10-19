package derive

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/labstack/driga/internal/model"
)

// InboxItems returns deduplicated events, acknowledging imports at or before
// baseline. Workers' completions are worker_finished records on the top-level
// parent, as are their attention requests. Event IDs retain the worker identity.
// Codex notify hooks are deliberately ignored: rollouts already emit completions.
func InboxItems(sessions []model.Session, parents map[int64]int64, hooks []model.HookEvent, baseline time.Time) []model.InboxItem {
	var result []model.InboxItem
	for _, item := range inboxEvents(sessions, hooks, baseline) {
		root := inboxRoot(item.SessionID, parents)
		if root != item.SessionID && item.Kind == "finished" {
			item.Kind = "worker_finished"
		}
		item.SessionID = root
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
		if !ts.After(baseline) {
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
		switch h.Event {
		case "Stop":
			kind = "finished"
		case "Notification":
			kind = "waiting"
		default:
			continue
		}
		add(h.Adapter, h.SessionID, fmt.Sprint(h.TS.UnixNano()), id, kind, h.TS, h.Message)
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
