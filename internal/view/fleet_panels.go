package view

import (
	"slices"
	"strings"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/pathx"
	"github.com/labstack/driga/internal/store"
)

// SessionPanel sends only outcomes and previously captured path metadata.
type SessionPanel struct {
	ID        int64
	MachineID string
	Turns     []string
	Files     []string
}
type ToolHour struct {
	TS                          time.Time
	Project, MachineID, Machine string
	Calls                       int
}

// FleetPanels reuses stored events, completions and edits. No transcript text or
// file contents are read, and paths outside a known project are withheld.
func FleetPanels(r Reader, f Filter, now time.Time) ([]SessionPanel, []ToolHour, error) {
	var details []SessionPanel
	var hours []ToolHour
	start := now.UTC().Truncate(time.Hour).Add(-23 * time.Hour)
	err := machineReaders(r, f.Scope, func(mr Reader, m model.Machine) error {
		ss, err := mr.Sessions(store.SessionFilter{EndedSince: start, RepoPath: f.Project})
		if err != nil {
			return err
		}
		repos, err := mr.Repos()
		if err != nil {
			return err
		}
		wts, err := mr.Worktrees(0)
		if err != nil {
			return err
		}
		files, err := mr.SessionFiles(time.Time{})
		if err != nil {
			return err
		}
		inbox, err := mr.Inbox(time.Time{}, true)
		if err != nil {
			return err
		}
		totals := map[string][]int{}
		for _, s := range ss {
			events, err := mr.PanelEvents(s.Adapter, s.ExternalID, now.Add(-7*24*time.Hour))
			if err != nil {
				return err
			}
			d := SessionPanel{ID: s.ID, MachineID: m.MachineID, Turns: turnOutcomes(events, inbox, s.ID, now)}
			root := ""
			for _, repo := range repos {
				if s.RepoID != nil && repo.ID == *s.RepoID {
					root = repo.Path
				}
			}
			// A worktree's root gives the same project-relative paths as the main checkout.
			for _, w := range wts {
				if s.WorktreeID != nil && w.ID == *s.WorktreeID {
					root = w.Path
				}
			}
			seen := map[string]bool{}
			if root != "" {
				for _, file := range files[s.ID] {
					if file.TS.After(now) {
						continue
					}
					rel, err := pathx.RecordedRel(root, file.Path)
					if err == nil && rel != "." && !seen[rel] {
						d.Files = append(d.Files, rel)
						seen[rel] = true
					}
				}
			}
			slices.Sort(d.Files)
			details = append(details, d)
			project := projectName(s, repos)
			if project == "" {
				continue
			}
			if totals[project] == nil {
				totals[project] = make([]int, 24)
			}
			// Transcript calls, hook results and OTLP can describe the same calls.
			// Use the largest observed channel count per session/hour, never their sum.
			log, hook, otlp := make([]int, 24), make([]int, 24), make([]int, 24)
			for _, e := range events {
				if e.TS.Before(start) || e.TS.After(now) {
					continue
				}
				i := int(e.TS.Sub(start) / time.Hour)
				if strings.HasPrefix(e.Kind, "log:tool_call:") {
					log[i]++
					continue
				}
				switch e.Kind {
				case "PostToolUse", "PostToolUseFailure", "postToolUse", "postToolUseFailure":
					hook[i]++
				case "otlp:tool_result":
					otlp[i]++
				}
			}
			for i := range hook {
				n := max(log[i], hook[i], otlp[i])
				totals[project][i] += n
			}
		}
		projects := make([]string, 0, len(totals))
		for p := range totals {
			projects = append(projects, p)
		}
		slices.Sort(projects)
		for _, p := range projects {
			for i, n := range totals[p] {
				hours = append(hours, ToolHour{start.Add(time.Duration(i) * time.Hour), p, m.MachineID, MachineName(m), n})
			}
		}
		return nil
	})
	return details, hours, err
}

// turnOutcomes merges terminal hooks and adapter completions. Waits belong to
// their actual interval; a failure is a terminal error or an interrupted open
// turn, never a tool failure that the agent subsequently recovered from.
func turnOutcomes(events []model.Event, inbox []model.InboxItem, id int64, now time.Time) []string {
	type endpoint struct {
		at      time.Time
		failed  bool
		channel string
	}
	var ends []endpoint
	var prompt time.Time
	open := false
	for _, e := range events {
		if e.TS.After(now) {
			continue
		}
		switch e.Kind {
		case "UserPromptSubmit", "userPromptSubmitted", "otlp:user_prompt", "log:turn_start":
			prompt = e.TS
			open = true
		case "Stop", "agentStop", "log:turn_end":
			channel := "hook"
			if e.Kind == "log:turn_end" {
				channel = "log"
			}
			ends = append(ends, endpoint{at: e.TS, channel: channel})
			open = false
		case "errorOccurred", "log:turn_failed":
			ends = append(ends, endpoint{at: e.TS, failed: true, channel: "hook"})
			open = false
		case "SessionEnd", "sessionEnd":
			if open && !prompt.IsZero() {
				ends = append(ends, endpoint{at: e.TS, failed: true, channel: "hook"})
				open = false
			}
		}
	}
	slices.SortFunc(ends, func(a, b endpoint) int { return a.at.Compare(b.at) })
	// The transcript and Stop hook can report the same completion a few
	// milliseconds apart. Separate channels without an intervening prompt
	// describe one turn; consecutive completions in one channel remain distinct.
	compact := ends[:0]
	for _, end := range ends {
		duplicate := false
		if len(compact) > 0 {
			prev := compact[len(compact)-1]
			if end.at.Equal(prev.at) {
				compact[len(compact)-1].failed = end.failed || prev.failed
				continue
			}
			duplicate = (end.at.Equal(prev.at) || end.channel != prev.channel) && end.failed == prev.failed
			for _, e := range events {
				if (e.Kind == "UserPromptSubmit" || e.Kind == "userPromptSubmitted" || e.Kind == "log:turn_start" || e.Kind == "otlp:user_prompt") && e.TS.After(prev.at) && !e.TS.After(end.at) {
					duplicate = false
					break
				}
			}
		}
		if !duplicate {
			compact = append(compact, end)
		}
	}
	ends = compact
	var out []string
	var previous time.Time
	for _, end := range ends {
		outcome := "ok"
		for _, i := range inbox {
			if i.SessionID == id && i.Kind == "waiting" && i.TS.After(previous) && !i.TS.After(end.at) {
				outcome = "waited"
			}
		}
		if end.failed {
			outcome = "failed"
		}
		out = append(out, outcome)
		previous = end.at
	}
	if len(out) > 30 {
		out = out[len(out)-30:]
	}
	return out
}
