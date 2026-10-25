package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/dashboard"
	drigamcp "github.com/labstack/driga/internal/mcp"
	"github.com/labstack/driga/internal/panel"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func panelSession(t *testing.T) *sdk.ClientSession {
	t.Helper()
	o := drigamcp.Options{Dashboards: dashboard.New(t.TempDir()), Panels: func(_ context.Context, req panel.RunRequest) (panel.Fragment, error) {
		if err := panel.Validate(req.Dashboard); err != nil {
			return panel.Fragment{}, err
		}
		f := panel.Fragment{Dashboard: req.Dashboard, ObservedAt: "2026-10-25T14:00:00Z"}
		for _, p := range req.Dashboard.Panels {
			f.Results = append(f.Results, panel.Result{ID: p.ID, Status: panel.StatusOK, Frame: &panel.Frame{Columns: []panel.Column{{Name: "value", Type: panel.Number, Unit: "count"}}, Rows: [][]any{{2.0}}}})
		}
		return f, nil
	}}
	ctx := context.Background()
	a, b := sdk.NewInMemoryTransports()
	ss, err := drigamcp.NewServer(o).Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test"}, nil).Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func callPanels(t *testing.T, cs *sdk.ClientSession, name string, args any) (*sdk.CallToolResult, map[string]any) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	raw, _ := json.Marshal(res.StructuredContent)
	_ = json.Unmarshal(raw, &out)
	return res, out
}

func TestPanelToolsRenderAsMCPApp(t *testing.T) {
	cs := panelSession(t)
	list, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	apps := map[string]bool{}
	for _, tool := range list.Tools {
		if ui, ok := tool.Meta["ui"].(map[string]any); ok && ui["resourceUri"] == drigamcp.PanelsAppURI {
			apps[tool.Name] = true
		}
	}
	for _, name := range []string{"show_panels", "open_dashboard", "create_dashboard", "edit_dashboard"} {
		if !apps[name] {
			t.Errorf("%s does not render as the panels app", name)
		}
	}
	resource, err := cs.ReadResource(context.Background(), &sdk.ReadResourceParams{URI: drigamcp.PanelsAppURI})
	if err != nil || len(resource.Contents) != 1 || resource.Contents[0].MIMEType != "text/html;profile=mcp-app" || resource.Contents[0].Text == "" {
		t.Fatalf("resource = %+v, %v", resource, err)
	}
}

func TestDashboardLifecycleOverMCP(t *testing.T) {
	cs := panelSession(t)
	res, out := callPanels(t, cs, "show_panels", map[string]any{"dashboard": map[string]any{"name": "Quick", "panels": []any{map[string]any{"id": "waits", "title": "Waits", "viz": "stat", "source": "inbox"}}}})
	if res.IsError || !strings.Contains(res.Content[0].(*sdk.TextContent).Text, "Waits: 2") {
		t.Fatalf("show = %+v", res.Content)
	}
	res, out = callPanels(t, cs, "create_dashboard", map[string]any{"dashboard": map[string]any{"name": "Payments", "panels": []any{map[string]any{"id": "waits", "title": "Waits", "viz": "stat", "source": "inbox"}}}, "message": "first"})
	id, _ := out["id"].(string)
	if res.IsError || !strings.HasPrefix(id, "payments-") || out["version"] != 1.0 {
		t.Fatalf("create = %+v %v", res.Content, out)
	}
	res, out = callPanels(t, cs, "edit_dashboard", map[string]any{"id": id, "base_version": 1, "operations": []any{map[string]any{"op": "add", "panel": map[string]any{"id": "mem", "title": "Memory", "viz": "stat", "source": "sessions", "measure": "sum:mem_bytes"}}}})
	if res.IsError || out["version"] != 2.0 {
		t.Fatalf("edit = %+v %v", res.Content, out)
	}
	res, _ = callPanels(t, cs, "edit_dashboard", map[string]any{"id": id, "base_version": 1, "operations": []any{map[string]any{"op": "rename", "name": "x"}}})
	if !res.IsError {
		t.Fatal("stale edit succeeded")
	}
	res, _ = callPanels(t, cs, "create_dashboard", map[string]any{"dashboard": map[string]any{"name": "Bad", "panels": []any{map[string]any{"id": "x", "title": "X", "viz": "pie", "source": "inbox"}}}})
	if !res.IsError || !strings.Contains(res.Content[0].(*sdk.TextContent).Text, "panels[0].viz") {
		t.Fatalf("invalid create = %+v", res.Content)
	}
	_, out = callPanels(t, cs, "list_dashboards", map[string]any{})
	if dashboards, _ := out["dashboards"].([]any); len(dashboards) != 2 {
		t.Fatalf("list = %v", out)
	}
	_, out = callPanels(t, cs, "restore_dashboard_version", map[string]any{"id": id, "version": 1})
	if out["version"] != 3.0 {
		t.Fatalf("restore = %v", out)
	}
	_, out = callPanels(t, cs, "open_dashboard", map[string]any{})
	if out["dashboard"].(map[string]any)["name"] != "Home" {
		t.Fatalf("home = %v", out)
	}
}
