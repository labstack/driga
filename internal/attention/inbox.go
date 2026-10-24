// Package attention manages attention policy, notifications, terminal destinations and jumps.
package attention

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/scrub"
)

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

// CommandRunner is wired only at production entry points. Fixtures inject a runner.
type CommandRunner struct{}

// ConfigureCommand applies native command-line rules to generated terminal
// commands. Other commands are left to os/exec's normal argv handling.
func ConfigureCommand(cmd *exec.Cmd) { configureCommand(cmd) }

func (CommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	configureCommand(cmd)
	cmd.WaitDelay = 50 * time.Millisecond
	cmd.Env = append(os.Environ(), "DRIGA_INTERNAL=1")
	// Bounded stdout; stderr is never exposed (it can contain agent text).
	var b limitedBuffer
	cmd.Stdout = &b
	cmd.Stderr = io.Discard
	var err error
	if runtime.GOOS == "windows" && name == "claude" {
		// The collector's bounded `claude agents` probe owns all descendants.
		// Terminal launch commands intentionally outlive their launcher instead.
		err = platform.RunChild(cmd)
	} else {
		err = cmd.Run()
	}
	return b.data, err
}

type limitedBuffer struct{ data []byte }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if len(b.data)+n > 1<<20 {
		return 0, fmt.Errorf("inbox command output exceeds 1 MiB")
	}
	b.data = append(b.data, p...)
	return n, nil
}

type Agent struct{ ID, State, Reason, Cwd string }

// PollAgents has one deadline for the entire CLI call, including startup.
func PollAgents(ctx context.Context, r Runner, budget time.Duration) ([]Agent, error) {
	if r == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	b, err := r.Run(ctx, "claude", "agents", "--json")
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	var rows []struct {
		ID             string          `json:"id"`
		SessionID      string          `json:"sessionId"`
		SessionIDSnake string          `json:"session_id"`
		State          string          `json:"state"`
		WaitingFor     json.RawMessage `json:"waitingFor"`
		Cwd            string          `json:"cwd"`
	}
	// Accept the documented list and a versioned wrapper; reject malformed data.
	if err = json.Unmarshal(b, &rows); err != nil {
		var wrapper struct {
			Agents json.RawMessage `json:"agents"`
		}
		if e := json.Unmarshal(b, &wrapper); e != nil || wrapper.Agents == nil {
			return nil, err
		}
		if err = json.Unmarshal(wrapper.Agents, &rows); err != nil {
			return nil, err
		}
	}
	out := make([]Agent, 0, len(rows))
	for _, v := range rows {
		id := v.SessionID
		if id == "" {
			id = v.SessionIDSnake
		}
		if id == "" {
			id = v.ID
		}
		if id == "" {
			continue
		}
		var waiting string
		if json.Unmarshal(v.WaitingFor, &waiting) != nil {
			var w struct {
				Type string `json:"type"`
			}
			json.Unmarshal(v.WaitingFor, &w)
			waiting = w.Type
		}
		reason := "unknown"
		switch waiting {
		case "permission", "permission_prompt":
			reason = "permission"
		case "question", "agent_needs_input":
			reason = "question"
		case "elicitation", "elicitation_dialog":
			reason = "elicitation"
		}
		out = append(out, Agent{ID: id, State: v.State, Reason: reason, Cwd: v.Cwd})
	}
	return out, nil
}

func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

type Pane struct{ TTY, Target, Server string }

func ReadPanes(ctx context.Context, r Runner) []Pane {
	if r == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	// The default server is the one reached through the viewer's TMUX environment.
	b, err := r.Run(ctx, "tmux", "list-panes", "-a", "-F", "#{pane_tty} #{session_name}:#{window_index}.#{pane_index} #{socket_path}")
	if err != nil {
		return nil
	}
	var out []Pane
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || !strings.HasPrefix(f[0], "/dev/") {
			continue
		}
		server := ""
		if len(f) == 3 {
			server = f[2]
		}
		out = append(out, Pane{f[0], f[1], server})
	}
	return out
}

func ResolveJump(s model.Session, tty string, panes []Pane) *model.InboxJump {
	if tty != "" {
		for _, p := range panes {
			if p.TTY == tty {
				return &model.InboxJump{Kind: "tmux", Target: p.Target, Server: p.Server, Command: tmuxAttach(p), Cwd: s.Cwd}
			}
		}
	}
	command := ""
	switch s.Adapter {
	case "claude":
		command = "claude --resume " + Quote(s.ExternalID)
	case "codex":
		command = "codex resume " + Quote(s.ExternalID)
	case "copilot":
		if !strings.HasPrefix(s.ExternalID, "pid:") {
			command = "copilot --resume=" + Quote(s.ExternalID)
		}
	}
	if command == "" {
		return nil
	}
	return &model.InboxJump{Kind: "resume", Command: command, Cwd: s.Cwd}
}
func tmuxAttach(p Pane) string {
	args := "tmux "
	if p.Server != "" {
		args += "-S " + Quote(p.Server) + " "
	}
	return args + "attach -t " + Quote(p.Target)
}

// Policy is applied before persistence, again on exports and notifications.
func Policy(i model.InboxItem, policy string) model.InboxItem {
	if MachineWarning(i.Kind) {
		i.Summary = scrub.Scrub(i.Summary)
		return i
	}
	i.Tool = scrub.Snippet(i.Tool)
	if policy == "none" {
		i.Tool = ""
	}
	if policy != "snippets" {
		i.Detail = ""
		if i.Kind == "waiting" {
			i.Summary = "waiting for input"
		} else {
			i.Summary = "turn finished"
		}
	} else {
		i.Detail = scrub.Snippet(i.Detail)
		i.Summary = scrub.Snippet(i.Summary)
	}
	return i
}

func Active(i model.InboxItem) bool {
	return i.Kind == "waiting" && i.AckedAt == nil && i.EndedAt == nil
}
func Less(a, b model.InboxItem) bool {
	aw, bw := Active(a), Active(b)
	if aw != bw {
		return aw
	}
	if a.TS.IsZero() != b.TS.IsZero() {
		return !a.TS.IsZero()
	}
	if !a.TS.Equal(b.TS) {
		if aw {
			return a.TS.Before(b.TS)
		}
		return a.TS.After(b.TS)
	}
	if a.ID != b.ID {
		return a.ID < b.ID
	}
	return a.MachineID < b.MachineID
}
func Waited(i model.InboxItem, now time.Time) time.Duration {
	if i.Kind != "waiting" {
		return 0
	}
	start := i.WaitingSinceAt
	if start.IsZero() {
		start = i.TS
	}
	if start.IsZero() {
		return 0
	}
	end := now
	if i.EndedAt != nil && i.EndedAt.Before(end) {
		end = *i.EndedAt
	}
	if i.AckedAt != nil && i.AckedAt.Before(end) {
		end = *i.AckedAt
	}
	return max(0, end.Sub(start))
}

func waitStart(i model.InboxItem) time.Time {
	if !i.WaitingSinceAt.IsZero() {
		return i.WaitingSinceAt
	}
	return i.TS
}

type Notification struct{ Kind, Title, Body, Sound, ItemID, MachineID string }
type Notifier interface {
	Notify(context.Context, Notification) error
}
type DesktopNotifier struct {
	Toast  func(context.Context, string) error // Native Windows backend; fixtures inject a recorder.
	Mac    Notifier                            // Native macOS backend; fixtures inject a fake.
	Runner Runner
	OS     string
}

func (d DesktopNotifier) Notify(ctx context.Context, n Notification) error {
	if d.OS == "windows" || d.OS == "" && runtime.GOOS == "windows" {
		if d.Toast == nil {
			return fmt.Errorf("notifications unavailable: native Windows toast backend not configured")
		}
		payload, err := toastPayload(n)
		if err != nil {
			return err
		}
		return d.Toast(ctx, payload)
	}
	goos := d.OS
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos == "darwin" {
		if d.Mac == nil {
			return fmt.Errorf("notifications unavailable: install Driga.app for native macOS notifications")
		}
		return d.Mac.Notify(ctx, n)
	}
	if d.Runner == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var err error
	switch goos {
	case "linux":
		sound := "message-new-instant"
		if n.Kind == "finished" {
			sound = "complete"
		}
		_, err = d.Runner.Run(ctx, "notify-send", "--app-name=Driga", "--hint=string:category:"+n.Kind, "--hint=string:sound-name:"+sound, n.Title, n.Body)
	}
	return err
}

// PlanAlerts marks suppressed items too: changing detail must not re-alert a wait.
// Waiting starts within three seconds form a summary. Health items in a committed
// batch coalesce per machine, regardless of the original episode timestamps.
func PlanAlerts(items []model.InboxItem, sessions []model.Session, c config.InboxConfig, policy string, now time.Time, seen, last map[string]time.Time) []Notification {
	type sessionKey struct {
		machine string
		id      int64
	}
	labels := map[sessionKey]string{}
	surfaces := map[sessionKey]string{}
	for _, s := range sessions {
		label := ""
		if policy != "none" {
			label = strings.TrimSpace(s.Title)
		}
		if label == "" && s.Cwd != "" {
			label = filepath.Base(s.Cwd)
		}
		labels[sessionKey{s.MachineID, s.ID}] = scrub.Snippet(label)
		surfaces[sessionKey{s.MachineID, s.ID}] = scrub.Snippet(s.StartedFrom)
	}
	text := func(i model.InboxItem) string {
		i = Policy(i, policy)
		label := labels[sessionKey{i.MachineID, i.SessionID}]
		if label == "" {
			label = "Session"
		}
		reason := strings.ReplaceAll(i.Reason, "_", " ")
		if i.Kind == "finished" {
			reason = "turn finished"
		} else if reason == "" || reason == "unknown" {
			reason = "waiting for input"
		}
		body := label + " · " + reason
		if i.Tool != "" {
			body += " (" + i.Tool + ")"
		}
		if i.Detail != "" {
			body += " · " + i.Detail
		} else if policy == "snippets" && i.Kind == "finished" && i.Summary != "" && i.Summary != reason {
			body += " · " + i.Summary
		}
		if surface := surfaces[sessionKey{i.MachineID, i.SessionID}]; surface != "" {
			body += " · from " + surface
		}
		return body
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].TS.Before(items[j].TS) })
	var waiting []model.InboxItem
	var notes []Notification
	healthByMachine := map[string][]model.InboxItem{}
	var healthOrder []string
	flushHealth := func(health []model.InboxItem) {
		if len(health) == 0 {
			return
		}
		n := Notification{Kind: health[0].Kind, Title: "Machine needs attention", Sound: "Glass", ItemID: health[0].ID, MachineID: health[0].MachineID}
		bodies := []string{}
		for _, i := range health {
			bodies = append(bodies, healthNotificationBody(i))
		}
		if len(health) == 1 {
			if health[0].Kind == "low_disk" {
				n.Title = "Low disk space"
			} else {
				n.Title = "Critical memory pressure"
			}
		} else {
			// A summary has no single destination.
			n.Kind = "machine_health"
			n.ItemID = ""
		}
		n.Body = strings.Join(bodies, " · ")
		notes = append(notes, n)
	}
	flush := func() {
		if len(waiting) == 0 {
			return
		}
		n := Notification{Kind: "waiting", Title: "Needs you", Sound: "Glass"}
		if len(waiting) > 1 {
			n.Body = fmt.Sprintf("%d sessions waiting", len(waiting))
			var apps []string
			known := map[string]bool{}
			for _, i := range waiting {
				app := surfaces[sessionKey{i.MachineID, i.SessionID}]
				if app != "" && !known[app] {
					known[app] = true
					apps = append(apps, app)
				}
			}
			if len(apps) > 0 {
				n.Body += " · from " + strings.Join(apps, ", ")
			}
		} else {
			n.Body, n.ItemID = text(waiting[0]), waiting[0].ID
			n.MachineID = waiting[0].MachineID
		}
		notes = append(notes, n)
		waiting = nil
	}
	for _, i := range items {
		key := i.MachineID + ":" + i.ID
		if _, ok := seen[key]; ok {
			continue
		}
		// alert_after delays a wait's alert until it has lasted that long, so a
		// prompt answered at the terminal never pings. Until then it stays unseen.
		open := i.AckedAt == nil && i.EndedAt == nil
		if open && i.Kind == "waiting" && c.AlertAfter > 0 && now.Sub(waitStart(i)) < c.AlertAfter && c.Alerts != "off" && c.Alerts != "" {
			continue
		}
		seen[key] = now
		if c.Alerts == "off" || c.Alerts == "" || !open {
			continue
		}
		if i.Kind != "waiting" && !MachineWarning(i.Kind) && !(i.Kind == "finished" && c.Alerts == "all") {
			continue
		}
		sk := AlertRepeatKey(i)
		if prev, ok := last[sk]; ok && !MachineWarning(i.Kind) && now.Sub(prev) < c.AlertRepeat {
			continue
		}
		last[sk] = now
		if MachineWarning(i.Kind) {
			if _, ok := healthByMachine[i.MachineID]; !ok {
				healthOrder = append(healthOrder, i.MachineID)
			}
			healthByMachine[i.MachineID] = append(healthByMachine[i.MachineID], i)
			continue
		}
		if i.Kind == "finished" {
			notes = append(notes, Notification{Kind: "finished", Title: "Finished", Body: text(i), Sound: "Pop", ItemID: i.ID, MachineID: i.MachineID})
			continue
		}
		if len(waiting) > 0 && i.TS.Sub(waiting[0].TS) > 3*time.Second {
			flush()
		}
		waiting = append(waiting, i)
	}
	flush()
	for _, machine := range healthOrder {
		flushHealth(healthByMachine[machine])
	}
	return notes
}

// MachineWarning identifies the two machine-only warning kinds.
func MachineWarning(kind string) bool { return kind == "low_disk" || kind == "memory_pressure" }

// AlertRepeatKey separates machine warnings from agent-session throttles.
func AlertRepeatKey(i model.InboxItem) string {
	if MachineWarning(i.Kind) {
		return i.MachineID + ":health:" + i.Kind
	}
	return i.MachineID + ":" + strconv.FormatInt(i.SessionID, 10)
}

// The inbox keeps the explanation and commands; a banner gets only the condition.
func healthNotificationBody(i model.InboxItem) string {
	text := strings.Join(strings.Fields(scrub.Scrub(i.Summary)), " ")
	if i.Kind == "memory_pressure" {
		return "Critical memory pressure; open Machine to inspect apps"
	}
	if first, _, ok := strings.Cut(text, ". "); ok {
		text = first
	}
	return strings.TrimSuffix(text, ".")
}
