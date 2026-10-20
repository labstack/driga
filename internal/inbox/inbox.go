// Package inbox contains bounded, injectable read-only attention integrations.
package inbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/scrub"
)

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

// CommandRunner is wired only at production entry points. Fixtures inject a runner.
type CommandRunner struct{}

func (CommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 50 * time.Millisecond
	cmd.Env = append(os.Environ(), "DRIGA_INTERNAL=1")
	// Bounded stdout; stderr is never exposed (it can contain agent text).
	var b limitedBuffer
	cmd.Stdout = &b
	cmd.Stderr = io.Discard
	err := cmd.Run()
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

type Notification struct{ Kind, Title, Body, Sound string }
type Notifier interface {
	Notify(context.Context, Notification) error
}
type DesktopNotifier struct {
	Runner Runner
	OS     string
}

func (d DesktopNotifier) Notify(ctx context.Context, n Notification) error {
	if d.Runner == nil {
		return nil
	}
	goos := d.OS
	if goos == "" {
		goos = runtime.GOOS
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var err error
	switch goos {
	case "darwin":
		_, err = d.Runner.Run(ctx, "osascript", "-e", "on run argv\ndisplay notification (item 1 of argv) with title (item 2 of argv) sound name (item 3 of argv)\nend run", n.Body, n.Title, n.Sound)
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
// Consecutive starts within three seconds in a committed batch form one summary.
func PlanAlerts(items []model.InboxItem, activity map[int64]time.Time, c config.InboxConfig, policy string, now time.Time, seen, last map[string]time.Time) []Notification {
	sort.SliceStable(items, func(i, j int) bool { return items[i].TS.Before(items[j].TS) })
	var waiting []model.InboxItem
	var notes []Notification
	flush := func() {
		if len(waiting) == 0 {
			return
		}
		n := Notification{Kind: "waiting", Title: "Needs input", Sound: "Glass"}
		if len(waiting) > 1 {
			n.Body = fmt.Sprintf("%d sessions waiting", len(waiting))
		} else {
			i := Policy(waiting[0], policy)
			n.Body = i.Reason
			if n.Body == "" {
				n.Body = "unknown"
			}
			if i.Tool != "" {
				n.Body += " · " + i.Tool
			}
			if i.Detail != "" {
				n.Body += " · " + i.Detail
			}
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
		if i.Kind != "waiting" && !(i.Kind == "finished" && c.Alerts == "all") {
			continue
		}
		sk := i.MachineID + ":" + strconv.FormatInt(i.SessionID, 10)
		if prev, ok := last[sk]; ok && now.Sub(prev) < c.AlertRepeat {
			continue
		}
		last[sk] = now
		if i.Kind == "finished" {
			i = Policy(i, policy)
			notes = append(notes, Notification{Kind: "finished", Title: "Done", Body: i.Summary, Sound: "Pop"})
			continue
		}
		if len(waiting) > 0 && i.TS.Sub(waiting[0].TS) > 3*time.Second {
			flush()
		}
		waiting = append(waiting, i)
	}
	flush()
	return notes
}
