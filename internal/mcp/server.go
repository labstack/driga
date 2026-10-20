// Package mcp exposes Driga's read-only views over the Model Context Protocol.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/jsonschema-go/jsonschema"
	"log/slog"
	"reflect"
	"sort"
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

const readDescription = ` Use this to inspect collected state without changing it; safe to retry. Optional scope fields select machine (default all) and project where supported; for example {"limit":100}. Returns a schema-2 envelope with data, UTC observed_at, source freshness and coverage gaps; mode: "snapshot" means no history. limit bounds total array entries, including nested collections (default 100, maximum 500). truncated=true means entries were omitted: narrow the scope or increase limit. Reading never acknowledges inbox items.`

// NewServer registers read tools, without resources or prompts.
func NewServer(o Options) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "driga", Version: o.Version}, &sdk.ServerOptions{Logger: o.Logger})
	s.AddReceivingMiddleware(preserveEnvelope)
	register(s, "get_flow", "Inspect one session family map: per-node stored mixer, events, inbox wait spans, observations, retention, gaps and machine-wide waiting attention. Optional id from list_agents selects its lead; omit for the newest live interactive session. Missing telemetry measurements are omitted; usage covers retained api_request events only.", func(ctx context.Context, in struct {
		ID      int64  `json:"id,omitempty"`
		Minutes int    `json:"minutes,omitempty" jsonschema:"Time window: 15, 60 or 240 minutes; default 60."`
		Machine string `json:"machine,omitempty"`
		Project string `json:"project,omitempty"`
	}) (view.Envelope, error) {
		if in.ID < 0 {
			return view.Envelope{}, fmt.Errorf("id must be positive")
		}
		return o.Read(ctx, Query{Machine: in.Machine, Project: in.Project}, func(r view.Reader, f view.Filter, now time.Time) (any, error) {
			f.FlowMinutes = in.Minutes
			return view.Flow(r, f, in.ID, now)
		})
	})
	register(s, "get_activity", "Read the last sixty minutes of machine samples and event counts.", func(ctx context.Context, in machineInput) (view.Envelope, error) {
		return o.Read(ctx, Query{Machine: in.Machine}, func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Activity(r, f, now) })
	})
	register(s, "get_summary", "Read agent counts, inbox counts, and machine health.", func(ctx context.Context, in machineInput) (view.Envelope, error) {
		return o.Read(ctx, Query{Machine: in.Machine}, func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Summary(r, f, now) })
	})
	register(s, "list_agents", "Read agent sessions and their families.", func(ctx context.Context, in agentsInput) (view.Envelope, error) {
		return o.Read(ctx, Query{Machine: in.Machine, Project: in.Project, Filter: view.Filter{All: in.All}}, func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Agents(r, f, now) })
	})
	register(s, "get_agent", "Read one session's detail and recent evidence; use required id from list_agents, e.g. id=1.", func(ctx context.Context, in agentInput) (view.Envelope, error) {
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
	register(s, "list_projects", "Read project activity and risk flags.", func(ctx context.Context, in projectsInput) (view.Envelope, error) {
		return o.Read(ctx, Query{Machine: in.Machine, Filter: view.Filter{AtRisk: in.AtRisk, Active: in.Active}}, func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Projects(r, f, now) })
	})
	register(s, "inspect_project", "Read a project's sourced status, worktrees, and handoffs; required project is a name or path, e.g. project=api.", func(ctx context.Context, in whereInput) (view.Envelope, error) {
		if in.Project == "" {
			return view.Envelope{}, fmt.Errorf("project is required")
		}
		return o.Read(ctx, Query{Project: in.Project}, func(r view.Reader, f view.Filter, now time.Time) (any, error) {
			return view.Where(r, f.Project, now, f.Scope)
		})
	})
	register(s, "list_inbox", "Read attention and completion events.", func(ctx context.Context, in inboxInput) (view.Envelope, error) {
		return o.Read(ctx, Query{Machine: in.Machine, Project: in.Project, Since: in.Since, Filter: view.Filter{All: in.All}}, func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Inbox(r, f, now) })
	})
	register(s, "list_conflicts", "Read current and recent file overlaps.", func(ctx context.Context, in conflictsInput) (view.Envelope, error) {
		return o.Read(ctx, Query{Machine: in.Machine, Project: in.Project}, func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Conflicts(r, f, now) })
	})
	register(s, "list_orphans", "Read orphan process candidates without signaling processes.", func(ctx context.Context, in machineInput) (view.Envelope, error) {
		return o.Read(ctx, Query{Machine: in.Machine}, func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Orphans(r, f, now) })
	})
	register(s, "get_machine", "Read machine health and agent memory use.", func(ctx context.Context, in machineInput) (view.Envelope, error) {
		return o.Read(ctx, Query{Machine: in.Machine}, func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Machine(r, f, now) })
	})
	register(s, "list_machines", "Read cached fleet connection health, even when collection has stopped.", func(ctx context.Context, _ struct{}) (view.Envelope, error) {
		return o.Read(ctx, Query{CachedMachines: true}, func(r view.Reader, f view.Filter, now time.Time) (any, error) { return view.Machines(r, now, f.Scope) })
	})
	register(s, "inspect_setup", "Read setup and collection diagnostics.", func(ctx context.Context, _ struct{}) (view.Envelope, error) {
		return o.Doctor(ctx)
	})
	register(s, "list_cleanup_candidates", "Inspect local disk cleanup candidates read-only. Optional kind is worktree, scratch, or history (omit for all); sizes=true opts into bounded directory sizing. This tool never removes anything.", func(ctx context.Context, in struct {
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
	schema, err := jsonschema.ForType(reflect.TypeFor[In](), nil)
	if err != nil {
		panic(err)
	}
	if schema.Properties == nil {
		schema.Properties = map[string]*jsonschema.Schema{}
	}
	minLimit, maxLimit := float64(1), float64(500)
	schema.Properties["limit"] = &jsonschema.Schema{Type: "integer", Minimum: &minLimit, Maximum: &maxLimit, Default: json.RawMessage("100"), Description: "Maximum total array entries, including nested collections; 1–500, default 100."}
	if name == "get_agent" || name == "get_flow" {
		schema.Properties["id"].Minimum = &minLimit
	}
	if name == "get_flow" {
		schema.Properties["minutes"].Enum = []any{15, 60, 240}
		schema.Properties["minutes"].Default = json.RawMessage("60")
	}
	if name == "inspect_project" {
		n := 1
		schema.Properties["project"].MinLength = &n
	}
	if name == "list_cleanup_candidates" {
		schema.Properties["kind"].Enum = []any{"worktree", "scratch", "history"}
	}
	sdk.AddTool(s, &sdk.Tool{Name: name, InputSchema: schema, Description: description + readDescription, Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, req *sdk.CallToolRequest, args map[string]any) (*sdk.CallToolResult, view.Envelope, error) {
		limit := 100
		if n, ok := args["limit"].(float64); ok {
			if n < 1 || n > 500 || n != float64(int(n)) {
				return nil, view.Envelope{}, fmt.Errorf("limit must be an integer from 1 to 500")
			}
			limit = int(n)
		}
		// Decode domain identifiers from the original JSON rather than the SDK's
		// validated float64 map, which may round integers above 2^53.
		b := []byte(req.Params.Arguments)
		if len(b) == 0 {
			b = []byte("{}")
		}
		var err error
		var in In
		if err = json.Unmarshal(b, &in); err != nil {
			return nil, view.Envelope{}, err
		}
		e, err := read(ctx, in)
		if err != nil {
			return nil, e, err
		}
		b, err = json.Marshal(e.Data)
		if err != nil {
			return nil, e, err
		}
		decoder := json.NewDecoder(bytes.NewReader(b))
		decoder.UseNumber()
		if err = decoder.Decode(&e.Data); err != nil {
			return nil, e, err
		}
		truncated := false
		e.Data = bounded(e.Data, &limit, &truncated)
		if len(e.CoverageGaps) > limit {
			e.CoverageGaps = e.CoverageGaps[:limit]
			truncated = true
		}
		e.Truncated = &truncated

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
	text := name
	if data, ok := e.Data.(map[string]any); ok {
		if rows, ok := data["rows"].([]any); ok {
			text += fmt.Sprintf(": %d rows", len(rows))
		} else if agents, ok := data["agents"]; ok && name == "get_summary" {
			text += fmt.Sprintf(": %v agents, %v inbox, %v orphans", agents, data["inbox"], data["orphans"])
		} else if name == "list_conflicts" {
			current, _ := data["current"].([]any)
			recent, _ := data["recent"].([]any)
			text += fmt.Sprintf(": %d current, %d recent", len(current), len(recent))
		}
	} else if checks, ok := e.Data.([]any); ok && name == "inspect_setup" {
		text += fmt.Sprintf(": %d checks", len(checks))
	}
	if e.Mode == "snapshot" {
		return text + " · snapshot · no history"
	}
	return text + " · " + e.Mode
}

// bounded uses a shared budget so families and multi-machine responses cannot
// multiply the requested maximum. Map traversal is sorted for repeatable cuts.
func bounded(v any, remaining *int, truncated *bool) any {
	switch v := v.(type) {
	case []any:
		out := make([]any, 0, min(len(v), *remaining))
		for _, item := range v {
			if *remaining == 0 {
				*truncated = true
				break
			}
			*remaining--
			out = append(out, bounded(item, remaining, truncated))
		}
		return out
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			// Fixed measurement vectors are part of a node, not collection
			// entries. Preserve their 60-bucket contract even at limit=1.
			if k == "observed" {
				continue
			}
			v[k] = bounded(v[k], remaining, truncated)
		}
	}
	return v
}
