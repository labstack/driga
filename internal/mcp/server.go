// Package mcp exposes Driga's read-only views over the Model Context Protocol.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/labstack/driga/internal/view"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// MaxLineLength bounds inbound stdio frames to four MiB.
const MaxLineLength = 4 * 1024 * 1024

// Builder builds a view inside the caller's read transaction.
type Builder func(view.Reader, view.Filter, time.Time) (any, error)

// Query supplies CLI-equivalent scope and view flags to a read.
type Query struct {
	Machine, Project, Since string
	Filter                  view.Filter
	CachedMachines          bool
}

// Options connects tool handlers to the shared CLI read path and diagnostics.
type Options struct {
	Version string
	Logger  *slog.Logger
	Read    func(context.Context, Query, Builder) (view.Envelope, error)
	Doctor  func(context.Context) (view.Envelope, error)
	Cleanup func(context.Context, string, bool) (view.Envelope, error)
}

type machineInput struct {
	Machine string `json:"machine,omitempty" jsonschema:"Machine name, ID, device-ID prefix, local, or all (default all)."`
}

type agentsInput struct {
	Project string `json:"project,omitempty" jsonschema:"Project name, path, or dot for the local working directory."`
	All     bool   `json:"all,omitempty" jsonschema:"Include sessions ended in the last 24 hours."`
	Machine string `json:"machine,omitempty" jsonschema:"Machine name, ID, device-ID prefix, local, or all."`
}

type agentInput struct {
	ID      int64  `json:"id" jsonschema:"Internal session ID shown by driga agents."`
	Machine string `json:"machine,omitempty" jsonschema:"Machine name, ID, device-ID prefix, local, or all."`
}

type projectsInput struct {
	AtRisk  bool   `json:"at_risk,omitempty" jsonschema:"Only projects with risk flags."`
	Active  bool   `json:"active,omitempty" jsonschema:"Only active projects."`
	Machine string `json:"machine,omitempty" jsonschema:"Machine name, ID, device-ID prefix, local, or all."`
}

type whereInput struct {
	Project string `json:"project" jsonschema:"Project name or path."`
}

type inboxInput struct {
	Since   string `json:"since,omitempty" jsonschema:"Non-negative duration or yesterday."`
	All     bool   `json:"all,omitempty" jsonschema:"Include acknowledged events."`
	Project string `json:"project,omitempty" jsonschema:"Project name, path, or dot for the local working directory."`
	Machine string `json:"machine,omitempty" jsonschema:"Machine name, ID, device-ID prefix, local, or all."`
}

type conflictsInput struct {
	Project string `json:"project,omitempty" jsonschema:"Project name, path, or dot for the local working directory."`
	Machine string `json:"machine,omitempty" jsonschema:"Machine name, ID, device-ID prefix, local, or all."`
}

const readDescription = ` Reading never acknowledges inbox items. mode: "snapshot" means no history.`

// NewServer registers exactly the eleven v1.5 read tools, without resources or prompts.
func NewServer(o Options) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "driga", Version: o.Version}, &sdk.ServerOptions{Logger: o.Logger})
	s.AddReceivingMiddleware(preserveEnvelope)
	register(s, "driga_summary", "Read agent counts, inbox counts, and machine health.", func(ctx context.Context, in machineInput) (view.Envelope, error) {
		return o.Read(ctx, Query{Machine: in.Machine}, func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Summary(r, f, now) })
	})
	register(s, "driga_agents", "Read agent sessions and their families.", func(ctx context.Context, in agentsInput) (view.Envelope, error) {
		return o.Read(ctx, Query{Machine: in.Machine, Project: in.Project, Filter: view.Filter{All: in.All}}, func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Agents(r, f, now) })
	})
	register(s, "driga_agent", "Read one session's detail and recent evidence.", func(ctx context.Context, in agentInput) (view.Envelope, error) {
		return o.Read(ctx, Query{Machine: in.Machine}, func(r view.Reader, f view.Filter, now time.Time) (any, error) {
			v, err := view.Agent(r, in.ID, now, f.Scope)
			if err == nil && f.Project != "" {
				repos, e := r.ReadMachine(v.MachineID).Repos()
				if e != nil {
					return v, e
				}
				for _, repo := range repos {
					if repo.Path == f.Project && v.Session.RepoID != nil && *v.Session.RepoID == repo.ID {
						return v, nil
					}
				}
				return v, fmt.Errorf("agent %d is outside project %q", in.ID, f.Project)
			}
			return v, err
		})
	})
	register(s, "driga_projects", "Read project activity and risk flags.", func(ctx context.Context, in projectsInput) (view.Envelope, error) {
		return o.Read(ctx, Query{Machine: in.Machine, Filter: view.Filter{AtRisk: in.AtRisk, Active: in.Active}}, func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Projects(r, f, now) })
	})
	register(s, "driga_where", "Read a project's sourced status, worktrees, and handoffs.", func(ctx context.Context, in whereInput) (view.Envelope, error) {
		if in.Project == "" {
			return view.Envelope{}, fmt.Errorf("project is required")
		}
		return o.Read(ctx, Query{Project: in.Project}, func(r view.Reader, f view.Filter, now time.Time) (any, error) {
			return view.Where(r, f.Project, now, f.Scope)
		})
	})
	register(s, "driga_inbox", "Read attention and completion events.", func(ctx context.Context, in inboxInput) (view.Envelope, error) {
		return o.Read(ctx, Query{Machine: in.Machine, Project: in.Project, Since: in.Since, Filter: view.Filter{All: in.All}}, func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Inbox(r, f, now) })
	})
	register(s, "driga_conflicts", "Read current and recent file overlaps.", func(ctx context.Context, in conflictsInput) (view.Envelope, error) {
		return o.Read(ctx, Query{Machine: in.Machine, Project: in.Project}, func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Conflicts(r, f, now) })
	})
	register(s, "driga_orphans", "Read orphan process candidates without signaling processes.", func(ctx context.Context, in machineInput) (view.Envelope, error) {
		return o.Read(ctx, Query{Machine: in.Machine}, func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Orphans(r, f, now) })
	})
	register(s, "driga_machine", "Read machine health and agent memory use.", func(ctx context.Context, in machineInput) (view.Envelope, error) {
		return o.Read(ctx, Query{Machine: in.Machine}, func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Machine(r, f, now) })
	})
	register(s, "driga_machines", "Read cached fleet connection health, even when collection has stopped.", func(ctx context.Context, _ struct{}) (view.Envelope, error) {
		return o.Read(ctx, Query{CachedMachines: true}, func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Machines(r, now, f.Scope) })
	})
	register(s, "driga_doctor", "Read setup and collection diagnostics.", func(ctx context.Context, _ struct{}) (view.Envelope, error) {
		return o.Doctor(ctx)
	})
	register(s, "driga_cleanup_candidates", "Inspect local disk cleanup candidates read-only. Sizes are opt-in; this tool never removes anything.", func(ctx context.Context, in struct {
		Kind  string `json:"kind,omitempty"`
		Sizes bool   `json:"sizes,omitempty"`
	}) (view.Envelope, error) {
		if in.Kind != "" && in.Kind != "worktree" && in.Kind != "scratch" && in.Kind != "history" {
			return view.Envelope{}, fmt.Errorf("unknown cleanup kind %q", in.Kind)
		}
		if o.Cleanup == nil {
			return view.Envelope{}, fmt.Errorf("cleanup inspection unavailable")
		}
		return o.Cleanup(ctx, in.Kind, in.Sizes)
	})
	return s
}

func register[In any](s *sdk.Server, name, description string, read func(context.Context, In) (view.Envelope, error)) {
	sdk.AddTool(s, &sdk.Tool{Name: name, Description: description + readDescription, Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, _ *sdk.CallToolRequest, in In) (*sdk.CallToolResult, view.Envelope, error) {
		e, err := read(ctx, in)
		if err != nil {
			return nil, e, err
		}
		if wire, ok := ctx.Value(envelopeKey{}).(*json.RawMessage); ok {
			*wire, err = json.Marshal(e)
			if err != nil {
				return nil, e, err
			}
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: summary(name, e)}}}, e, nil
	})
}

type envelopeKey struct{}

// SDK v1.8.0 validates typed output through a float64 map, rounding large
// integers. Retain the original envelope after validation so wire JSON stays
// identical to CLI JSON. The slot belongs to one request, including concurrent calls.
func preserveEnvelope(next sdk.MethodHandler) sdk.MethodHandler {
	return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
		if method != "tools/call" {
			return next(ctx, method, req)
		}
		var wire json.RawMessage
		result, err := next(context.WithValue(ctx, envelopeKey{}, &wire), method, req)
		if r, ok := result.(*sdk.CallToolResult); ok && err == nil && !r.IsError && wire != nil {
			r.StructuredContent = wire
		}
		return result, err
	}
}

func summary(name string, e view.Envelope) string {
	text := strings.TrimPrefix(name, "driga_")
	if data, ok := e.Data.(map[string]any); ok {
		if rows, ok := data["rows"].([]any); ok {
			text += fmt.Sprintf(": %d rows", len(rows))
		} else if agents, ok := data["agents"]; ok && name == "driga_summary" {
			text += fmt.Sprintf(": %v agents, %v inbox, %v orphans", agents, data["inbox"], data["orphans"])
		} else if name == "driga_conflicts" {
			current, _ := data["current"].([]any)
			recent, _ := data["recent"].([]any)
			text += fmt.Sprintf(": %d current, %d recent", len(current), len(recent))
		}
	} else if checks, ok := e.Data.([]any); ok && name == "driga_doctor" {
		text += fmt.Sprintf(": %d checks", len(checks))
	}
	if e.Mode == "snapshot" {
		return text + " · snapshot · no history"
	}
	return text + " · " + e.Mode
}
