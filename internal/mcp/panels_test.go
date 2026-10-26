package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/dashboard"
	drigamcp "github.com/labstack/driga/internal/mcp"
	"github.com/labstack/driga/internal/panel"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func panelSession(t *testing.T) *sdk.ClientSession {
	t.Helper()
	return panelSessionWith(t, nil)
}

func panelSessionWith(t *testing.T, configure func(*drigamcp.Options)) *sdk.ClientSession {
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
	if configure != nil {
		configure(&o)
	}
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
	if dashboards, _ := out["dashboards"].([]any); len(dashboards) != len(panel.BuiltinIDs)+1 {
		t.Fatalf("list = %v", out)
	}
	_, out = callPanels(t, cs, "restore_dashboard_version", map[string]any{"id": id, "version": 1, "base_version": 2})
	if out["version"] != 3.0 {
		t.Fatalf("restore = %v", out)
	}
	_, out = callPanels(t, cs, "open_dashboard", map[string]any{"id": panel.HeaderID})
	if d, _ := out["dashboard"].(map[string]any); d["name"] != "Header" {
		t.Fatalf("header = %v", out)
	}
	res, _ = callPanels(t, cs, "edit_dashboard", map[string]any{"id": panel.HeaderID, "base_version": 0, "operations": []any{map[string]any{"op": "add", "panel": map[string]any{"id": "t", "title": "T", "viz": "table", "source": "sessions"}}}})
	if !res.IsError {
		t.Fatal("header accepted a table")
	}
	if res, _ = callPanels(t, cs, "delete_dashboard", map[string]any{"id": id}); res.IsError {
		t.Fatalf("delete = %+v", res.Content)
	}
	if res, _ = callPanels(t, cs, "open_dashboard", map[string]any{"id": id}); !res.IsError {
		t.Fatal("opened a deleted dashboard")
	}
}

func testPanelSpec(name string) panel.Dashboard {
	return panel.Dashboard{Name: name, Panels: []panel.Panel{{ID: "waits", Title: "Waits", Viz: "stat", Source: "inbox"}}}
}

func TestSavedPanelTextCarriesTheStructuredIdentity(t *testing.T) {
	cs := panelSession(t)
	res, out := callPanels(t, cs, "create_dashboard", map[string]any{"dashboard": testPanelSpec("Saved")})
	id, _ := out["id"].(string)
	check := func(res *sdk.CallToolResult, out map[string]any, version float64) {
		t.Helper()
		var text map[string]any
		if res.IsError || len(res.Content) != 1 {
			t.Fatalf("save failed: %+v", res)
		}
		if err := json.Unmarshal([]byte(res.Content[0].(*sdk.TextContent).Text), &text); err != nil {
			t.Fatalf("server text is not the saved fragment JSON: %v", err)
		}
		a, _ := json.Marshal(text)
		b, _ := json.Marshal(out)
		if string(a) != string(b) || text["id"] != id || text["version"] != version {
			t.Fatalf("text and structured identity disagree: %s != %s", a, b)
		}
	}
	check(res, out, 1)
	res, out = callPanels(t, cs, "edit_dashboard", map[string]any{"id": id, "base_version": 1, "operations": []any{map[string]any{"op": "rename", "name": "Edited"}}})
	check(res, out, 2)
	res, out = callPanels(t, cs, "replace_dashboard", map[string]any{"id": id, "base_version": 2, "dashboard": testPanelSpec("Replaced")})
	check(res, out, 3)
	res, out = callPanels(t, cs, "restore_dashboard_version", map[string]any{"id": id, "version": 1, "base_version": 3})
	check(res, out, 4)
	res, out = callPanels(t, cs, "open_dashboard", map[string]any{"id": id})
	check(res, out, 4)
}

func TestSavedDashboardSurvivesLiveRenderFailure(t *testing.T) {
	for _, failure := range []string{"source", "encoding"} {
		t.Run(failure, func(t *testing.T) {
			var saved *dashboard.Store
			cs := panelSessionWith(t, func(o *drigamcp.Options) {
				saved = o.Dashboards
				o.Panels = func(_ context.Context, req panel.RunRequest) (panel.Fragment, error) {
					if failure == "source" {
						return panel.Fragment{}, errors.New("snapshot unavailable")
					}
					return panel.Fragment{Dashboard: req.Dashboard, Results: []panel.Result{{ID: "waits", Frame: &panel.Frame{Rows: [][]any{{math.NaN()}}}}}}, nil
				}
			})
			res, out := callPanels(t, cs, "create_dashboard", map[string]any{"dashboard": testPanelSpec("Committed")})
			if res.IsError || out["version"] != 1.0 {
				t.Fatalf("committed save reported failure: %+v %+v", res, out)
			}
			id, _ := out["id"].(string)
			record, err := saved.Get(id)
			if err != nil || record.Version != 1 {
				t.Fatalf("saved dashboard missing: %+v %v", record, err)
			}
			text := res.Content[0].(*sdk.TextContent).Text
			var response map[string]any
			if json.Unmarshal([]byte(text), &response) != nil || response["id"] != id || !strings.Contains(text, "live data unavailable") {
				t.Fatalf("save lost identity or warning: %s", text)
			}
		})
	}
}

func TestWholeDashboardWritesAreAnnotatedDestructive(t *testing.T) {
	cs := panelSession(t)
	list, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range list.Tools {
		if tool.Name == "replace_dashboard" || tool.Name == "restore_dashboard_version" {
			if tool.Annotations == nil || tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
				t.Fatalf("%s is not marked destructive", tool.Name)
			}
		}
	}
}

func TestDashboardWritesRequireAnExplicitCurrentVersion(t *testing.T) {
	cs := panelSessionWith(t, func(o *drigamcp.Options) { o.ChatServer, o.Chat = true, true })
	_, out := callPanels(t, cs, "create_dashboard", map[string]any{"dashboard": testPanelSpec("Versions")})
	id, _ := out["id"].(string)
	_, out = callPanels(t, cs, "edit_dashboard", map[string]any{"id": id, "base_version": 1, "operations": []any{map[string]any{"op": "rename", "name": "New version"}}})
	if out["version"] != 2.0 {
		t.Fatalf("fixture edit failed: %v", out)
	}
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"edit_dashboard", map[string]any{"id": id, "operations": []any{map[string]any{"op": "rename", "name": "Overwrite"}}}},
		{"replace_dashboard", map[string]any{"id": id, "dashboard": testPanelSpec("Overwrite")}},
		{"restore_dashboard_version", map[string]any{"id": id, "version": 1}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			res, _ := callPanels(t, cs, tc.tool, tc.args)
			if !res.IsError || !strings.Contains(res.Content[0].(*sdk.TextContent).Text, "base_version") {
				t.Fatalf("missing base version accepted: %+v", res)
			}
			tc.args["base_version"] = 1
			res, _ = callPanels(t, cs, tc.tool, tc.args)
			if !res.IsError {
				t.Fatalf("stale %s overwrote version 2", tc.tool)
			}
		})
	}
	res, _ := callPanels(t, cs, "edit_dashboard", map[string]any{"id": panel.HomeID, "operations": []any{map[string]any{"op": "rename", "name": "Unversioned home"}}})
	if !res.IsError {
		t.Fatal("omitted version silently edited an unsaved built-in")
	}
	res, out = callPanels(t, cs, "edit_dashboard", map[string]any{"id": panel.HomeID, "base_version": 0, "operations": []any{map[string]any{"op": "rename", "name": "Custom home"}}})
	if res.IsError || out["version"] != 1.0 {
		t.Fatalf("explicit version zero could not edit an unsaved built-in: %+v %+v", res, out)
	}
}

func TestChatDashboardCreatesAreCappedBeforeSaving(t *testing.T) {
	cs := panelSessionWith(t, func(o *drigamcp.Options) { o.ChatServer, o.Chat = true, true })
	var wg sync.WaitGroup
	results := make(chan bool, 2*chat.MaxDashboardCreates)
	for i := 0; i < 2*chat.MaxDashboardCreates; i++ {
		wg.Go(func() {
			res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "create_dashboard", Arguments: map[string]any{"dashboard": testPanelSpec("Automatic")}})
			results <- err == nil && !res.IsError
		})
	}
	wg.Wait()
	close(results)
	success := 0
	for ok := range results {
		if ok {
			success++
		}
	}
	if success != chat.MaxDashboardCreates {
		t.Fatalf("Chat created %d dashboards, limit is %d", success, chat.MaxDashboardCreates)
	}
	_, out := callPanels(t, cs, "list_dashboards", map[string]any{})
	if rows, _ := out["dashboards"].([]any); len(rows) != len(panel.BuiltinIDs)+chat.MaxDashboardCreates {
		t.Fatalf("create cap was applied after saving: %+v", out)
	}
}
