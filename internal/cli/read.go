package cli

import (
	"context"
	"errors"
	"fmt"
	"github.com/labstack/driga/internal/pathx"
	"github.com/labstack/driga/internal/secfile"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/gitscan"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

// SummaryCmd prints the default one-screen summary.
type SummaryCmd struct{ ReadOptions }

// AgentsCmd lists live sessions and their worker families.
type AgentsCmd struct {
	List AgentsListCmd `cmd:"" default:"withargs" help:"List agents grouped by family."`
	Show AgentCmd      `cmd:"" help:"Show one session by session ID."`
	Jump AgentsJumpCmd `cmd:"" help:"Confirm and jump to a local session."`
	Kill AgentsKillCmd `cmd:"" help:"Confirm and terminate a local idle session."`
}
type AgentsListCmd struct {
	ReadOptions
	All bool `help:"Include sessions ended within the last 24 hours."`
}

// AgentCmd shows one session without acknowledging any inbox events.
type AgentCmd struct {
	DetailOptions
	ID int64 `arg:"" name:"session-id" help:"Internal session ID."`
}

// ProjectsCmd lists repositories and their derived status.
type ProjectsCmd struct {
	List      ProjectsListCmd `cmd:"" default:"withargs" help:"List project status."`
	Show      WhereCmd        `cmd:"" help:"Show project status, worktrees and sessions."`
	Summarize SummarizeCmd    `cmd:"" help:"Refresh opt-in local project summaries through Claude."`
}
type ProjectsListCmd struct {
	ReadOptions
	AtRisk bool `help:"Only show projects with risk flags."`
	Active bool `help:"Only show active projects."`
}

// WhereCmd shows a project's sourced summary, worktrees and agents.
type WhereCmd struct {
	ProjectDetailOptions
	Project string `arg:"" help:"Repository name or path."`
}

// InboxCmd leaves room for explicit acknowledgment alongside the read command.
type InboxCmd struct {
	List InboxListCmd `cmd:"" default:"withargs" help:"List inbox events."`
	Ack  InboxAckCmd  `cmd:"" help:"Explicitly acknowledge inbox events."`
	Jump JumpCmd      `cmd:"" help:"Jump to a local inbox item without acknowledging it."`
}

// InboxListCmd lists active attention and completions, optionally including history.
type InboxListCmd struct {
	ReadOptions
	All bool `help:"Show all history, including acknowledged and ended events (default: current waits and up to 50 completions from 24h)."`
}

// OrphansCmd leaves room for explicit process actions alongside the read command.
type OrphansCmd struct {
	List OrphansListCmd `cmd:"" default:"withargs" help:"List orphan candidates."`
	Kill OrphansKillCmd `cmd:"" help:"Confirm and terminate an orphan tree."`
}

// OrphansListCmd lists orphan candidates and their evidence.
type OrphansListCmd struct{ OrphanOptions }

// MachineShowCmd shows health and top agent memory users.
type MachineShowCmd struct {
	MachineDetailOptions
	Machine string `arg:"" optional:"" default:"local" help:"Machine name, ID, device-ID prefix, or local."`
	Full    bool   `help:"Show all retained MCP trends and full process detail."`
}

// Run renders the summary.
func (SummaryCmd) Run(g *Globals) error {
	return runRead(g, view.Filter{}, "", func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Summary(r, f, now) })
}

// Run renders agent families.
func (c AgentsListCmd) Run(g *Globals) error {
	return runRead(g, view.Filter{All: c.All}, "", func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Agents(r, f, now) })
}

// Run renders session detail.
func (c AgentCmd) Run(g *Globals) error {
	if c.ID <= 0 {
		return invalidUsage("session-id must be positive")
	}
	return runRead(g, view.Filter{}, "", func(r view.Reader, f view.Filter, now time.Time) (any, error) {
		v, err := view.Agent(r, c.ID, now, f.Scope)
		if errors.Is(err, store.ErrNotFound) {
			return v, missingAgent(c.ID)
		}
		if err != nil {
			return v, err
		}
		if f.Project != "" {
			repos, err := r.ReadMachine(v.MachineID).Repos()
			if err != nil {
				return v, err
			}
			ok := false
			for _, repo := range repos {
				if pathx.Equal(repo.Path, f.Project) && v.Session.RepoID != nil && *v.Session.RepoID == repo.ID {
					ok = true
				}
			}
			if !ok {
				return v, fmt.Errorf("agent %d is outside project %q", c.ID, f.Project)
			}
		}
		return v, nil
	})
}

// Run renders project status rows.
func (c ProjectsListCmd) Run(g *Globals) error {
	return runRead(g, view.Filter{AtRisk: c.AtRisk, Active: c.Active}, "", func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Projects(r, f, now) })
}

// Run renders a project's details.
func (c WhereCmd) Run(g *Globals) error {
	return runRead(g, view.Filter{}, c.Project, func(r view.Reader, f view.Filter, now time.Time) (any, error) {
		return view.Where(r, f.Project, now, f.Scope)
	})
}

// Run renders inbox events.
func (c InboxListCmd) Run(g *Globals) error {
	return runRead(g, view.Filter{All: c.All}, "", func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Inbox(r, f, now) })
}

// Run renders orphan candidates.
func (OrphansListCmd) Run(g *Globals) error {
	return runRead(g, view.Filter{}, "", func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Orphans(r, f, now) })
}

// Run renders machine health.
func (c MachineShowCmd) Run(g *Globals) error {
	if c.Machine == "all" {
		return invalidUsage("machines show requires one machine; all is not supported")
	}
	if c.Machine != "" {
		g.Machine = c.Machine
	} else {
		g.Machine = "local"
	}
	return runRead(g, view.Filter{Full: c.Full}, "", func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Machine(r, f, now) })
}

type readBuilder func(view.Reader, view.Filter, time.Time) (any, error)

func runRead(g *Globals, base view.Filter, project string, build readBuilder) error {
	if _, err := parseSince(g.Since, time.Now()); err != nil {
		return err
	}
	ctx, stop := notifyContext(context.Background())
	defer stop()
	err := readOnce(ctx, g, base, project, build)
	if ctx.Err() != nil {
		return nil
	}
	return err
}

func readOnce(ctx context.Context, g *Globals, base view.Filter, project string, build readBuilder) error {
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	base.CaptureText = cfg.CaptureText
	st, mode, closeFn, err := OpenReader(ctx, g)
	if err != nil {
		return err
	}
	defer closeFn()
	now := time.Now()
	f := base
	f.Since, err = parseSince(g.Since, now)
	if err != nil {
		return err
	}
	if project == "" {
		project = g.Project
	}

	// One read transaction keeps all view rows and metadata on the same snapshot.
	var data any
	var envelope view.Envelope
	err = st.Tx(ctx, func(tx *store.Tx) error {
		var err error
		f, err = resolveViewFilter(g, tx, f, project)
		if err != nil {
			return err
		}
		data, err = build(tx, f, now)
		if err != nil {
			return err
		}
		age := time.Duration(1<<63 - 1)
		if info, err := os.Stat(filepath.Join(config.DataDir(), "viewer.heartbeat")); err == nil {
			age = now.Sub(info.ModTime())
		}
		envelope, err = view.NewEnvelope(tx, mode, now, collector.Cadence(age), data, f.Scope)
		if err != nil {
			return err
		}
		if !f.Since.IsZero() {
			envelope.CoverageGaps, err = view.CoverageGaps(tx, f.Since, f.Scope)
			if envelope.CoverageGaps == nil {
				envelope.CoverageGaps = []model.Gap{}
			}
		}
		return err
	})
	if err != nil {
		return err
	}
	return view.Render(g.Stdout, data, view.RenderOpts{JSON: g.JSON, NoColor: g.NoColor, Envelope: envelope})
}

func touchHeartbeat() error {
	dir := config.DataDir()
	if err := secfile.MkdirPrivate(dir); err != nil {
		return err
	}
	f, err := secfile.OpenFile(filepath.Join(dir, "viewer.heartbeat"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		return err
	}
	return f.Close()
}

func parseSince(s string, now time.Time) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if s == "yesterday" {
		y, m, d := now.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, now.Location()).AddDate(0, 0, -1), nil
	}
	dur, err := time.ParseDuration(s)
	if err != nil || dur < 0 {
		return time.Time{}, invalidUsage("invalid --since %q: use a non-negative duration or yesterday", s)
	}
	return now.Add(-dur), nil
}

func resolveProject(r view.Reader, input string) (string, error) {
	repos, err := r.Repos()
	if err != nil {
		return "", err
	}
	_, statErr := os.Stat(config.ExpandHome(input))
	if input == "." || strings.ContainsAny(input, "/\\") || statErr == nil {
		path := gitscan.NormalizePath(config.ExpandHome(input))
		best := ""
		longest := 0
		for _, repo := range repos {
			root := gitscan.NormalizePath(repo.Path)
			if pathWithin(path, root) && len(root) > longest {
				best = repo.Path
				longest = len(root)
			}
		}
		wts, err := r.Worktrees(0)
		if err != nil {
			return "", err
		}
		for _, wt := range wts {
			root := gitscan.NormalizePath(wt.Path)
			if pathWithin(path, root) && len(root) > longest {
				for _, repo := range repos {
					if repo.ID == wt.RepoID {
						best = repo.Path
						longest = len(root)
					}
				}
			}
		}
		if best != "" {
			return best, nil
		}
		return "", fmt.Errorf("unknown project path %q", input)
	}
	matches := []string{}
	for _, repo := range repos {
		if repo.Name == input {
			matches = append(matches, repo.Path)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("unknown project %q", input)
	case 1:
		return matches[0], nil
	default:
		sort.Strings(matches)
		return "", fmt.Errorf("ambiguous project %q: %s", input, strings.Join(matches, ", "))
	}
}

func pathWithin(path, root string) bool { return pathx.Within(path, root) }

// FlowCmd reads a family; omitted ID selects the newest live interactive session.
type FlowCmd struct {
	ReadOptions
	Minutes int    `default:"60" enum:"15,60,240" help:"Time window: 15, 60 or 240 minutes."`
	ID      *int64 `arg:"" name:"session-id" optional:"" help:"Internal session ID (defaults to most recently active live interactive session)."`
}

func (c FlowCmd) Run(g *Globals) error {
	id := int64(0)
	if c.ID != nil {
		id = *c.ID
		if id <= 0 {
			return invalidUsage("session-id must be positive")
		}
	}
	if c.Minutes == 0 {
		c.Minutes = 60
	}
	if c.Minutes != 15 && c.Minutes != 60 && c.Minutes != 240 {
		return invalidUsage("--minutes must be 15, 60 or 240")
	}
	if c.ID != nil && g.Since != "" {
		return invalidUsage("--since selects the default session; omit the session ID or --since")
	}

	return runRead(g, view.Filter{FlowMinutes: c.Minutes}, "", func(r view.Reader, f view.Filter, now time.Time) (any, error) {
		v, err := view.Flow(r, f, id, now)
		if errors.Is(err, store.ErrNotFound) {
			return v, missingAgent(id)
		}
		return v, err
	})
}
