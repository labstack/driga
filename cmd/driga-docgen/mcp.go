package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/cli"
	drigamcp "github.com/labstack/driga/internal/mcp"
)

// listTools asks a server for its tools/list answer over an in-memory
// transport: the same question a connecting agent asks, rather than a reading
// of the registration calls.
func listTools(server *sdk.Server) ([]*sdk.Tool, error) {
	ctx := context.Background()
	a, b := sdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, a, nil)
	if err != nil {
		return nil, err
	}
	defer ss.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "driga-docgen", Version: "docs"}, nil).Connect(ctx, b, nil)
	if err != nil {
		return nil, err
	}
	defer cs.Close()
	var tools []*sdk.Tool
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func renderMCP() (string, error) {
	// `driga mcp` serves exactly this server.
	read, err := listTools(cli.NewMCPServer(&cli.Globals{Stderr: io.Discard}))
	if err != nil {
		return "", err
	}
	// Chat's private `mcp --chat` child adds prompts and action tools.
	full, err := listTools(drigamcp.NewServer(drigamcp.Options{
		Chat:    true,
		Prompts: func(context.Context) ([]chat.Prompt, error) { return nil, nil },
	}))
	if err != nil {
		return "", err
	}
	readNames := map[string]bool{}
	for _, tool := range read {
		readNames[tool.Name] = true
	}
	// Panel and dashboard tools return fragments and may save dashboards;
	// they get their own section.
	var panels []*sdk.Tool
	served := read[:0:0]
	for _, tool := range read {
		if slices.Contains(drigamcp.DashboardTools, tool.Name) {
			panels = append(panels, tool)
		} else {
			served = append(served, tool)
		}
	}
	read = served
	var chatOnly []*sdk.Tool
	for _, tool := range full {
		if !readNames[tool.Name] {
			chatOnly = append(chatOnly, tool)
		}
	}
	for _, tools := range [][]*sdk.Tool{read, panels, chatOnly} {
		sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	}
	for _, tool := range read {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			return "", fmt.Errorf("tool %s is served by `driga mcp` without readOnlyHint, but the page states that server is read-only", tool.Name)
		}
	}

	var b strings.Builder
	frontmatter(&b,
		"MCP tools",
		"Every tool driga's MCP server exposes, with its inputs, generated from the server's tools/list answer.",
		fmt.Sprintf("The %s and %s `driga mcp` serves to your agents and the %s only Driga's own Chat receives, with their inputs.", plural(len(read), "read-only tool"), plural(len(panels), "panel and dashboard tool"), plural(len(chatOnly), "tool")),
		"You are connecting an agent to driga over MCP and want to know what it can call.",
		"You need a tool's exact input fields, defaults or limits.",
	)
	b.WriteString("`driga mcp` serves read tools and panel tools over stdio. Every read tool\n")
	b.WriteString("answers with the same schema-2 view envelope as the CLI's `--json` output, and reading never\n")
	b.WriteString("acknowledges inbox items. Without a running collector the tools answer from a\n")
	b.WriteString("labelled one-shot snapshot with no history. Registration is covered in\n")
	b.WriteString("[Connect an agent over MCP](/guides/mcp).\n\n")

	suffix := sharedSuffix(read)
	if suffix != "" {
		b.WriteString("Every read tool shares this contract:\n\n")
		fmt.Fprintf(&b, "> %s\n\n", text(suffix))
	}

	fmt.Fprintf(&b, "## Read tools\n\nServed by `driga mcp`. All %d are annotated read-only.\n\n", len(read))
	toolIndex(&b, read)
	for _, tool := range read {
		if err := renderTool(&b, tool, suffix); err != nil {
			return "", err
		}
	}

	fmt.Fprintf(&b, "## Panel and dashboard tools\n\n")
	b.WriteString("Served by `driga mcp`. Each returns a dashboard with its live results,\n")
	b.WriteString("which hosts that support MCP Apps render as an interactive card from\n")
	b.WriteString("`ui://driga/panels.html`; other hosts show its text summary. The writes\n")
	b.WriteString("change only Driga's own dashboards: every save is a version that\n")
	b.WriteString("`restore_dashboard_version` can bring back. `run_panels` is called only by the\n")
	b.WriteString("rendered card, to refresh it.\n\n")
	toolIndex(&b, panels)
	for _, tool := range panels {
		if err := renderTool(&b, tool, ""); err != nil {
			return "", err
		}
	}

	b.WriteString("## Chat tools\n\n")
	b.WriteString("Offered only to Driga's own Chat turn through a private MCP child, never by\n")
	b.WriteString("`driga mcp`. Action tools run through Chat's approval cards and trust modes;\n")
	b.WriteString("see [Chat and actions](/guides/chat).\n\n")
	toolIndex(&b, chatOnly)
	for _, tool := range chatOnly {
		if err := renderTool(&b, tool, ""); err != nil {
			return "", err
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n", nil
}

func toolIndex(b *strings.Builder, tools []*sdk.Tool) {
	b.WriteString("| Tool | Read-only |\n|---|---|\n")
	for _, tool := range tools {
		ro := "no"
		if tool.Annotations != nil && tool.Annotations.ReadOnlyHint {
			ro = "yes"
		}
		fmt.Fprintf(b, "| [%s](#%s) | %s |\n", code(tool.Name), tool.Name, ro)
	}
	b.WriteString("\n")
}

type schemaProp struct {
	Type        json.RawMessage `json:"type"`
	Description string          `json:"description"`
	Enum        []any           `json:"enum"`
	Default     json.RawMessage `json:"default"`
	Minimum     *float64        `json:"minimum"`
	Maximum     *float64        `json:"maximum"`
	MinLength   *int            `json:"minLength"`
}

type objectSchema struct {
	Properties map[string]schemaProp `json:"properties"`
	Required   []string              `json:"required"`
}

func renderTool(b *strings.Builder, tool *sdk.Tool, suffix string) error {
	fmt.Fprintf(b, "### %s\n\n", tool.Name)
	description := strings.TrimSpace(strings.TrimSuffix(tool.Description, suffix))
	fmt.Fprintf(b, "%s\n\n", text(description))
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		return err
	}
	var schema objectSchema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return fmt.Errorf("%s input schema: %w", tool.Name, err)
	}
	if len(schema.Properties) == 0 {
		b.WriteString("No inputs.\n\n")
		return nil
	}
	required := map[string]bool{}
	for _, name := range schema.Required {
		required[name] = true
	}
	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	b.WriteString("| Input | Type | Required | Description |\n|---|---|---|---|\n")
	for _, name := range names {
		p := schema.Properties[name]
		req := ""
		if required[name] {
			req = "yes"
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s |\n", code(name), schemaType(p.Type), req, propDescription(p))
	}
	b.WriteString("\n")
	return nil
}

func schemaType(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "any"
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		return strings.Join(many, " or ")
	}
	return string(raw)
}

func propDescription(p schemaProp) string {
	parts := []string{}
	if p.Description != "" {
		parts = append(parts, cell(sentence(p.Description)))
	}
	// Limits the description already states are not repeated.
	said := strings.ToLower(p.Description)
	if len(p.Enum) > 0 && !mentionsAll(said, p.Enum) {
		values := make([]string, len(p.Enum))
		for i, v := range p.Enum {
			values[i] = code(fmt.Sprint(v))
		}
		parts = append(parts, "One of "+strings.Join(values, ", ")+".")
	}
	switch {
	case p.Minimum != nil && p.Maximum != nil && !strings.Contains(said, fmt.Sprintf("%g", *p.Maximum)):
		parts = append(parts, fmt.Sprintf("Range %g–%g.", *p.Minimum, *p.Maximum))
	case p.Minimum != nil && p.Maximum == nil:
		parts = append(parts, fmt.Sprintf("Minimum %g.", *p.Minimum))
	}
	if p.MinLength != nil && *p.MinLength > 0 {
		parts = append(parts, "Must not be empty.")
	}
	if len(p.Default) > 0 && !strings.Contains(said, "default") {
		parts = append(parts, "Default "+code(string(p.Default))+".")
	}
	return strings.Join(parts, " ")
}

func mentionsAll(text string, values []any) bool {
	for _, v := range values {
		if !strings.Contains(text, strings.ToLower(fmt.Sprint(v))) {
			return false
		}
	}
	return true
}

// sharedSuffix returns the closing sentences every tool description repeats,
// so the page states that contract once. It is cut at a sentence boundary.
func sharedSuffix(tools []*sdk.Tool) string {
	if len(tools) < 2 {
		return ""
	}
	suffix := tools[0].Description
	for _, tool := range tools[1:] {
		d := tool.Description
		n := 0
		for n < len(suffix) && n < len(d) && suffix[len(suffix)-1-n] == d[len(d)-1-n] {
			n++
		}
		suffix = suffix[len(suffix)-n:]
	}
	i := strings.Index(suffix, ". ")
	if i < 0 {
		if strings.HasPrefix(suffix, " ") {
			return strings.TrimSpace(suffix)
		}
		return ""
	}
	if strings.HasPrefix(suffix, " ") {
		return strings.TrimSpace(suffix)
	}
	return strings.TrimSpace(suffix[i+1:])
}
