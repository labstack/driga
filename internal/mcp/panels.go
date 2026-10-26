package mcp

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/dashboard"
	"github.com/labstack/driga/internal/panel"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// PanelsAppURI is the MCP App that renders every panel fragment.
	PanelsAppURI = "ui://driga/panels.html"
	appMIME      = "text/html;profile=mcp-app"
)

//go:embed apps/*.html
var appFiles embed.FS

// DashboardTools are the panel and dashboard tools. Their writes change only
// Driga's own dashboards, never agents, sessions or the machine.
var DashboardTools = []string{"describe_panels", "show_panels", "run_panels", "open_dashboard", "list_dashboards", "list_dashboard_versions", "create_dashboard", "edit_dashboard", "replace_dashboard", "restore_dashboard_version", "delete_dashboard"}

const specGuide = ` A dashboard is {"name", "variables"?, "panels": [...]}. Each panel has an id, title, viz and source; filter with where [{"column","op","value"}] where value may be $variable; bar panels group by a column; stat, gauge, bar and grouped tables aggregate with measure (count, sum:col, avg:col, max:col, min:col); timeseries reads the activity source per minute. Read describe_panels for every source's columns before drafting. Panels render as interactive cards in hosts that support MCP Apps and as text elsewhere.`

func appMeta() sdk.Meta {
	return sdk.Meta{"ui": map[string]any{"resourceUri": PanelsAppURI, "visibility": []string{"model", "app"}}}
}

// helperMeta marks tools only the rendered app calls, such as a refresh.
func helperMeta() sdk.Meta { return sdk.Meta{"ui": map[string]any{"visibility": []string{"app"}}} }

type showInput struct {
	Dashboard panel.Dashboard   `json:"dashboard" jsonschema:"Dashboard spec to run once without saving"`
	Vars      map[string]string `json:"vars,omitempty" jsonschema:"Variable values by name"`
}

type openInput struct {
	ID   string            `json:"id" jsonschema:"Dashboard id from list_dashboards"`
	Vars map[string]string `json:"vars,omitempty" jsonschema:"Variable values by name"`
}

type idInput struct {
	ID string `json:"id" jsonschema:"Dashboard id from list_dashboards"`
}

type createInput struct {
	Dashboard panel.Dashboard `json:"dashboard" jsonschema:"Complete dashboard spec"`
	Message   string          `json:"message,omitempty" jsonschema:"One line describing it, shown in history"`
}

type editInput struct {
	ID          string                `json:"id" jsonschema:"Dashboard id"`
	Operations  []dashboard.Operation `json:"operations" jsonschema:"Edits applied in order, all or none"`
	BaseVersion int                   `json:"base_version" jsonschema:"Required version you read, including 0 for an unsaved built-in; fails if someone saved since"`
	Message     string                `json:"message,omitempty" jsonschema:"One line describing the change, shown in history"`
}

type replaceInput struct {
	ID          string          `json:"id" jsonschema:"Dashboard id"`
	Dashboard   panel.Dashboard `json:"dashboard" jsonschema:"Complete replacement spec; panels it leaves out are removed"`
	BaseVersion int             `json:"base_version" jsonschema:"Required version you read, including 0 for an unsaved built-in; fails if someone saved since"`
	Message     string          `json:"message,omitempty" jsonschema:"One line describing the change, shown in history"`
}

type restoreInput struct {
	ID          string `json:"id" jsonschema:"Dashboard id"`
	Version     int    `json:"version" jsonschema:"Earlier version to restore; restoring saves it as the newest version"`
	BaseVersion int    `json:"base_version" jsonschema:"Required current version you read; fails if someone saved since"`
}

type describeOutput struct {
	Sources  []panel.SourceInfo `json:"sources"`
	Vizzes   []string           `json:"vizzes"`
	Measures string             `json:"measures"`
	Fields   []string           `json:"variable_fields"`
}

type listOutput struct {
	Dashboards []dashboard.Summary `json:"dashboards"`
}

type versionsOutput struct {
	Versions []versionSummary `json:"versions"`
}

type versionSummary struct {
	Version int              `json:"version"`
	SavedAt string           `json:"saved_at"`
	Author  dashboard.Author `json:"author"`
	Message string           `json:"message,omitempty"`
	Panels  int              `json:"panels"`
}

func registerPanels(s *sdk.Server, o Options) {
	readOnly := &sdk.ToolAnnotations{ReadOnlyHint: true}
	write := &sdk.ToolAnnotations{DestructiveHint: boolPtr(false), IdempotentHint: false, OpenWorldHint: boolPtr(false)}
	replace := &sdk.ToolAnnotations{DestructiveHint: boolPtr(true), IdempotentHint: false, OpenWorldHint: boolPtr(false)}
	var creates atomic.Int32
	run := func(ctx context.Context, req panel.RunRequest) (*sdk.CallToolResult, panel.Fragment, error) {
		f, err := o.Panels(ctx, req)
		if err != nil {
			return nil, panel.Fragment{}, err
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: describeFragment(f)}}}, f, nil
	}
	saved := func(ctx context.Context, verb string, r dashboard.Record, err error, vars map[string]string) (*sdk.CallToolResult, panel.Fragment, error) {
		if err != nil {
			return nil, panel.Fragment{}, dashboardError(err)
		}
		_, f, err := run(ctx, panel.RunRequest{Dashboard: r.Spec, Vars: vars})
		if err != nil {
			f = savedWarning(r, verb, err)
		}
		f.ID, f.Version = r.ID, r.Version
		raw, err := json.Marshal(f)
		if err != nil {
			f = savedWarning(r, verb, err)
			f.ID, f.Version = r.ID, r.Version
			// The fallback holds only the already-persisted spec and string
			// warnings, so it cannot contain an unsupported live reading.
			raw, _ = json.Marshal(f)
		}
		// Text and structured content deliberately carry the same fragment.
		// Chat can read the saved identity without a host-specific transform.
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(raw)}}}, f, nil
	}

	sdk.AddTool(s, &sdk.Tool{Name: "describe_panels", Title: "Panel sources", Annotations: readOnly,
		Description: "List the data sources a panel can show, with their columns and types, the panel kinds, measures and variable fields. Read this before drafting a panel." + specGuide,
	}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, describeOutput, error) {
		out := describeOutput{Sources: panel.Sources(), Vizzes: []string{"stat", "gauge", "timeseries", "bar", "table", "list", "inbox", "cards", "timeline", "text"}, Measures: "count, sum:<column>, avg:<column>, max:<column>, min:<column>", Fields: []string{"project", "machine", "agent", "label", "kind"}}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: fmt.Sprintf("%d sources: %s.", len(out.Sources), strings.Join(panel.SourceNames(), ", "))}}}, out, nil
	})
	sdk.AddTool(s, &sdk.Tool{Name: "show_panels", Title: "Show panels", Annotations: readOnly, Meta: appMeta(),
		Description: "Show one or more panels of live agent data in chat without saving them, as an interactive card. Use this to answer with a chart, table or count." + specGuide,
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in showInput) (*sdk.CallToolResult, panel.Fragment, error) {
		return run(ctx, panel.RunRequest{Dashboard: in.Dashboard, Vars: in.Vars})
	})
	sdk.AddTool(s, &sdk.Tool{Name: "run_panels", Title: "Refresh panels", Annotations: readOnly, Meta: helperMeta(),
		Description: "Run a dashboard spec again with new variable values; the rendered card calls this to refresh.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in showInput) (*sdk.CallToolResult, panel.Fragment, error) {
		return run(ctx, panel.RunRequest{Dashboard: in.Dashboard, Vars: in.Vars})
	})
	sdk.AddTool(s, &sdk.Tool{Name: "open_dashboard", Title: "Open dashboard", Annotations: readOnly, Meta: appMeta(),
		Description: "Show a saved dashboard with live data.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in openInput) (*sdk.CallToolResult, panel.Fragment, error) {
		r, err := o.Dashboards.Get(in.ID)
		return saved(ctx, "Opened", r, err, in.Vars)
	})
	sdk.AddTool(s, &sdk.Tool{Name: "list_dashboards", Title: "Dashboards", Annotations: readOnly,
		Description: "List saved dashboards with their ids, panel counts and versions. The first, id header, holds the metrics the Driga app shows above Chat: only stat panels, at most 8; edit it when the user wants a number there. Next, id home, is the dashboard the app opens on, with Chat beside it.",
	}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, listOutput, error) {
		list, err := o.Dashboards.List()
		if err != nil {
			return nil, listOutput{}, err
		}
		names := make([]string, len(list))
		for i, d := range list {
			names[i] = fmt.Sprintf("%s (%s, %d panels)", d.Name, d.ID, d.Panels)
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: fmt.Sprintf("%d dashboards: %s.", len(list), strings.Join(names, "; "))}}}, listOutput{Dashboards: list}, nil
	})
	sdk.AddTool(s, &sdk.Tool{Name: "list_dashboard_versions", Title: "Dashboard history", Annotations: readOnly,
		Description: "List a dashboard's saved versions, newest first, with who saved each and why.",
	}, func(_ context.Context, _ *sdk.CallToolRequest, in idInput) (*sdk.CallToolResult, versionsOutput, error) {
		versions, err := o.Dashboards.Versions(in.ID)
		if err != nil {
			return nil, versionsOutput{}, dashboardError(err)
		}
		out := versionsOutput{Versions: make([]versionSummary, len(versions))}
		for i, v := range versions {
			out.Versions[i] = versionSummary{Version: v.Version, SavedAt: v.SavedAt.Format("2006-01-02T15:04:05Z"), Author: v.Author, Message: v.Message, Panels: len(v.Spec.Panels)}
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: fmt.Sprintf("%d versions.", len(versions))}}}, out, nil
	})
	agent := dashboard.Author{Kind: "agent"}
	// Chat gets dashboard writes only with its actions; it checks that the
	// server offers no tool beyond those (chat.Claude). Delete is not
	// pre-allowed there, so each one waits on the person's approval.
	if !o.ChatServer || o.Chat {
		sdk.AddTool(s, &sdk.Tool{Name: "create_dashboard", Title: "Create dashboard", Annotations: write, Meta: appMeta(),
			Description: "Save a new dashboard the user can open in the Driga app and edit. Returns it with live data." + specGuide,
		}, func(ctx context.Context, _ *sdk.CallToolRequest, in createInput) (*sdk.CallToolResult, panel.Fragment, error) {
			if o.ChatServer && creates.Add(1) > chat.MaxDashboardCreates {
				return nil, panel.Fragment{}, errors.New("too many dashboard creates in Chat turn")
			}
			r, err := o.Dashboards.Create(in.Dashboard, agent, in.Message)
			return saved(ctx, "Created", r, err, nil)
		})
		sdk.AddTool(s, &sdk.Tool{Name: "edit_dashboard", Title: "Edit dashboard", Annotations: write, Meta: appMeta(),
			Description: "Change a saved dashboard with typed operations: add, update (merge fields), remove, move, layout, variables, rename. Edits apply in order, all or none, as a new version." + specGuide,
		}, func(ctx context.Context, _ *sdk.CallToolRequest, in editInput) (*sdk.CallToolResult, panel.Fragment, error) {
			r, err := o.Dashboards.Edit(in.ID, in.Operations, in.BaseVersion, agent, in.Message)
			return saved(ctx, "Updated", r, err, nil)
		})
		sdk.AddTool(s, &sdk.Tool{Name: "replace_dashboard", Title: "Replace dashboard", Annotations: replace, Meta: appMeta(),
			Description: "Save a whole new spec for a dashboard as a new version; dragged positions of panels it keeps are preserved." + specGuide,
		}, func(ctx context.Context, _ *sdk.CallToolRequest, in replaceInput) (*sdk.CallToolResult, panel.Fragment, error) {
			r, err := o.Dashboards.Replace(in.ID, in.Dashboard, in.BaseVersion, agent, in.Message)
			return saved(ctx, "Replaced", r, err, nil)
		})
		sdk.AddTool(s, &sdk.Tool{Name: "restore_dashboard_version", Title: "Restore dashboard version", Annotations: replace, Meta: appMeta(),
			Description: "Save an earlier version of a dashboard as its newest version. Nothing is lost: the replaced version stays in history.",
		}, func(ctx context.Context, _ *sdk.CallToolRequest, in restoreInput) (*sdk.CallToolResult, panel.Fragment, error) {
			r, err := o.Dashboards.Restore(in.ID, in.Version, in.BaseVersion, agent)
			return saved(ctx, "Restored", r, err, nil)
		})
	}
	if !o.ChatServer || o.Chat {
		sdk.AddTool(s, &sdk.Tool{Name: "delete_dashboard", Title: "Delete dashboard", Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(true), OpenWorldHint: boolPtr(false)},
			Description: "Delete a saved dashboard and its history. Deleting header restores the default metrics. Only when the user asks.",
		}, func(_ context.Context, _ *sdk.CallToolRequest, in idInput) (*sdk.CallToolResult, struct{}, error) {
			if err := o.Dashboards.Delete(in.ID); err != nil {
				return nil, struct{}{}, dashboardError(err)
			}
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "Deleted " + in.ID + "."}}}, struct{}{}, nil
		})
	}
	addAppResource(s)
}

// savedWarning reports a committed save even when its live data could not be
// rendered. Retrying the save would otherwise create an unintended new version.
func savedWarning(r dashboard.Record, verb string, err error) panel.Fragment {
	f := panel.Fragment{Dashboard: r.Spec, ObservedAt: time.Now().UTC().Format(time.RFC3339), Results: []panel.Result{}}
	for _, p := range r.Spec.Panels {
		f.Results = append(f.Results, panel.Result{ID: p.ID, Status: panel.StatusError, Message: verb + " dashboard; live data unavailable: " + err.Error()})
	}
	return f
}

func addAppResource(s *sdk.Server) {
	meta := sdk.Meta{"ui": map[string]any{"csp": map[string]any{}}}
	s.AddResource(&sdk.Resource{Name: "Driga panels", Title: "Driga panels", URI: PanelsAppURI, MIMEType: appMIME, Meta: meta,
		Description: "Interactive Driga panels and dashboards delivered as an MCP App."},
		func(context.Context, *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
			html, err := appFiles.ReadFile("apps/panels.html")
			if err != nil {
				return nil, err
			}
			return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: PanelsAppURI, MIMEType: appMIME, Text: string(html), Meta: meta}}}, nil
		})
}

// describeFragment is the text a host without MCP Apps shows, and what the
// model reads: each panel's headline value or row count.
func describeFragment(f panel.Fragment) string {
	parts := make([]string, 0, len(f.Results))
	titles := map[string]string{}
	for _, p := range f.Dashboard.Panels {
		titles[p.ID] = p.Title
	}
	for _, r := range f.Results {
		title := titles[r.ID]
		switch {
		case r.Frame == nil:
			parts = append(parts, title)
		case r.Status == panel.StatusEmpty:
			parts = append(parts, title+": none")
		case len(r.Frame.Columns) == 1 && len(r.Frame.Rows) == 1:
			parts = append(parts, fmt.Sprintf("%s: %v", title, formatValue(r.Frame.Rows[0][0], r.Frame.Columns[0].Unit)))
		default:
			parts = append(parts, fmt.Sprintf("%s: %d rows", title, len(r.Frame.Rows)))
		}
	}
	return fmt.Sprintf("%s · %s · observed %s", f.Dashboard.Name, strings.Join(parts, "; "), f.ObservedAt)
}

func formatValue(v any, unit string) string {
	n, ok := v.(float64)
	if !ok {
		return fmt.Sprint(v)
	}
	switch unit {
	case "bytes":
		const mb = 1 << 20
		if n >= 1<<30 {
			return fmt.Sprintf("%.1f GB", n/(1<<30))
		}
		return fmt.Sprintf("%.0f MB", n/mb)
	case "duration":
		return fmt.Sprintf("%.0fs", n/1000)
	case "percent":
		return fmt.Sprintf("%.0f%%", n)
	}
	return fmt.Sprintf("%g", n)
}

func dashboardError(err error) error {
	var problems panel.Problems
	switch {
	case errors.As(err, &problems):
		return fmt.Errorf("invalid dashboard: %w", err)
	case errors.Is(err, dashboard.ErrStale), errors.Is(err, dashboard.ErrNotFound), errors.Is(err, dashboard.ErrVersion), errors.Is(err, dashboard.ErrAlreadyLatest):
		return err
	}
	return err
}

func boolPtr(b bool) *bool { return &b }
