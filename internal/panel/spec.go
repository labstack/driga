// Package panel defines Driga's dashboard spec and runs its panels over a
// presentation snapshot. The same spec renders as a dashboard in the app and
// as an inline card in any chat that hosts MCP Apps.
package panel

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// MaxPanels is the most panels one dashboard holds.
const MaxPanels = 40

// Dashboard is a named set of panels and the variables that scope them.
type Dashboard struct {
	Name        string     `json:"name" jsonschema:"Short dashboard name, at most 80 characters"`
	Description string     `json:"description,omitempty" jsonschema:"What the dashboard is for, at most 280 characters"`
	Variables   []Variable `json:"variables,omitempty" jsonschema:"Variables referenced as $name in panel filters and titles"`
	Panels      []Panel    `json:"panels" jsonschema:"Panels in display order"`
}

// Variable scopes every panel that filters on $name. Its options are the
// distinct values of Field across the snapshot.
type Variable struct {
	Name    string `json:"name" jsonschema:"Identifier referenced as $name: lowercase letters, digits and underscores"`
	Field   string `json:"field" jsonschema:"project, machine, agent, label or kind"`
	Default string `json:"default,omitempty" jsonschema:"Initial value; empty means all"`
}

// Panel is one visualization of one source.
type Panel struct {
	ID          string   `json:"id" jsonschema:"Stable identifier: lowercase letters, digits and underscores; edits address panels by id"`
	Title       string   `json:"title" jsonschema:"Panel title, at most 80 characters; may reference $variables"`
	Description string   `json:"description,omitempty" jsonschema:"Help text, at most 280 characters"`
	Viz         string   `json:"viz" jsonschema:"stat, gauge, timeseries, bar, table, list, inbox, cards, timeline or text; cards shows rows as tiles, timeline draws each session from start to end with its waits"`
	Source      string   `json:"source,omitempty" jsonschema:"sessions, flow, waits, inbox, projects, orphans, cleanup, activity, local_activity, memory, costs, machines, summary or activity_heatmap; every viz except text needs one"`
	Where       []Filter `json:"where,omitempty" jsonschema:"Filters on source columns, all of which must match"`
	By          string   `json:"by,omitempty" jsonschema:"Column to group rows by; bar and table group, other vizzes ignore it"`
	Measure     string   `json:"measure,omitempty" jsonschema:"count, or sum:, avg:, max: or min: followed by a numeric column, e.g. sum:mem_bytes; stat, gauge, bar and grouped tables aggregate with it"`
	Columns     []string `json:"columns,omitempty" jsonschema:"Columns to show, in order; table and list default to the source's own"`
	Sort        string   `json:"sort,omitempty" jsonschema:"Column to sort by; prefix with - for descending"`
	Limit       int      `json:"limit,omitempty" jsonschema:"Most rows to return, 1 to 200; default 50"`
	Unit        string   `json:"unit,omitempty" jsonschema:"count, bytes, percent, duration, usd or none; inferred from the measured column when omitted"`
	Max         *float64 `json:"max,omitempty" jsonschema:"gauge: the value that fills the gauge"`
	Total       string   `json:"total,omitempty" jsonschema:"stat: a second measure the value is out of, shown as value / total, e.g. max:mem_total_bytes"`
	Caption     string   `json:"caption,omitempty" jsonschema:"stat: a second measure shown after the value, e.g. sum:leftover"`
	Empty       string   `json:"empty,omitempty" jsonschema:"What the panel says when nothing matches: what would appear here and why, at most 160 characters"`
	Width       int      `json:"width,omitempty" jsonschema:"Grid columns from 1 to 12; default depends on viz"`
	Height      string   `json:"height,omitempty" jsonschema:"s, m or l; default depends on viz"`
	Content     string   `json:"content,omitempty" jsonschema:"text panels: Markdown, at most 4000 characters"`
	Grid        *Grid    `json:"grid,omitempty" jsonschema:"Position set by the app when panels are dragged; omit when authoring"`
}

// Filter keeps rows whose column matches. Value may be $variable.
type Filter struct {
	Column string `json:"column" jsonschema:"A column of the panel's source"`
	Op     string `json:"op,omitempty" jsonschema:"eq (default), ne, in, gt, lt or contains"`
	Value  string `json:"value" jsonschema:"Value to compare, or $variable; in takes a comma-separated list"`
}

// Grid places a panel on the 12-column dashboard grid.
type Grid struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

var (
	idPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	vizzes    = []string{"stat", "gauge", "timeseries", "bar", "table", "list", "inbox", "cards", "timeline", "text"}
	ops       = []string{"", "eq", "ne", "in", "gt", "lt", "contains"}
	heights   = []string{"", "s", "m", "l"}
	units     = []string{"", "count", "bytes", "percent", "duration", "usd", "none"}
	fields    = []string{"project", "machine", "agent", "label", "kind"}
)

// Problem names one invalid field by its JSON path.
type Problem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Problems is every problem a spec has; it is an error.
type Problems []Problem

func (p Problems) Error() string {
	parts := make([]string, len(p))
	for i, problem := range p {
		parts[i] = problem.Path + ": " + problem.Message
	}
	return strings.Join(parts, "; ")
}

// Validate reports every problem with d, checking columns against the
// sources' schemas.
func Validate(d Dashboard) error {
	var problems Problems
	add := func(path, format string, args ...any) {
		problems = append(problems, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
	}
	if strings.TrimSpace(d.Name) == "" || utf8.RuneCountInString(d.Name) > 80 {
		add("name", "must be 1 to 80 characters")
	}
	if utf8.RuneCountInString(d.Description) > 280 {
		add("description", "must be at most 280 characters")
	}
	vars := map[string]bool{}
	for i, v := range d.Variables {
		path := fmt.Sprintf("variables[%d]", i)
		if !idPattern.MatchString(v.Name) {
			add(path+".name", "must be lowercase letters, digits and underscores, starting with a letter")
		} else if vars[v.Name] {
			add(path+".name", "%q is declared twice", v.Name)
		}
		vars[v.Name] = true
		if !slices.Contains(fields, v.Field) {
			add(path+".field", "must be one of %s", strings.Join(fields, ", "))
		}
	}
	if len(d.Panels) == 0 {
		add("panels", "a dashboard needs at least one panel")
	}
	if len(d.Panels) > MaxPanels {
		add("panels", "at most %d panels", MaxPanels)
	}
	ids := map[string]bool{}
	for i, p := range d.Panels {
		path := fmt.Sprintf("panels[%d]", i)
		if !idPattern.MatchString(p.ID) {
			add(path+".id", "must be lowercase letters, digits and underscores, starting with a letter")
		} else if ids[p.ID] {
			add(path+".id", "%q is used twice", p.ID)
		}
		ids[p.ID] = true
		if strings.TrimSpace(p.Title) == "" || utf8.RuneCountInString(p.Title) > 80 {
			add(path+".title", "must be 1 to 80 characters")
		}
		if utf8.RuneCountInString(p.Description) > 280 {
			add(path+".description", "must be at most 280 characters")
		}
		if !slices.Contains(vizzes, p.Viz) {
			add(path+".viz", "must be one of %s", strings.Join(vizzes, ", "))
			continue
		}
		if !slices.Contains(heights, p.Height) {
			add(path+".height", "must be s, m or l")
		}
		if !slices.Contains(units, p.Unit) {
			add(path+".unit", "must be one of %s", strings.Join(units[1:], ", "))
		}
		if p.Width < 0 || p.Width > 12 {
			add(path+".width", "must be 1 to 12")
		}
		if p.Limit < 0 || p.Limit > 200 {
			add(path+".limit", "must be 1 to 200")
		}
		if p.Caption != "" && p.Viz != "stat" {
			add(path+".caption", "only stat panels show a caption")
		}
		if p.Viz == "text" {
			if strings.TrimSpace(p.Content) == "" || utf8.RuneCountInString(p.Content) > 4000 {
				add(path+".content", "text panels need 1 to 4000 characters of Markdown")
			}
			if p.Source != "" {
				add(path+".source", "text panels have no source")
			}
			continue
		}
		schema, ok := sources[p.Source]
		if !ok {
			add(path+".source", "must be one of %s", strings.Join(SourceNames(), ", "))
			continue
		}
		has := func(column string) bool { return schema.column(column) != nil }
		for j, f := range p.Where {
			fp := fmt.Sprintf("%s.where[%d]", path, j)
			if !has(f.Column) {
				add(fp+".column", "%q is not a column of %s (%s)", f.Column, p.Source, schema.names())
			}
			if !slices.Contains(ops, f.Op) {
				add(fp+".op", "must be eq, ne, in, gt, lt or contains")
			}
			if name, ok := strings.CutPrefix(f.Value, "$"); ok && !vars[name] {
				add(fp+".value", "$%s is not a declared variable", name)
			}
		}
		if p.By != "" && !has(p.By) {
			add(path+".by", "%q is not a column of %s (%s)", p.By, p.Source, schema.names())
		}
		for j, c := range p.Columns {
			if !has(c) {
				add(fmt.Sprintf("%s.columns[%d]", path, j), "%q is not a column of %s (%s)", c, p.Source, schema.names())
			}
		}
		if sortBy := strings.TrimPrefix(p.Sort, "-"); sortBy != "" && !has(sortBy) && sortBy != "value" {
			add(path+".sort", "%q is not a column of %s (%s)", sortBy, p.Source, schema.names())
		}
		for field, m := range map[string]string{"measure": p.Measure, "total": p.Total, "caption": p.Caption} {
			if m == "" {
				continue
			}
			if _, column, err := parseMeasure(m); err != nil {
				add(path+"."+field, "%s", err)
			} else if column != "" {
				if c := schema.column(column); c == nil {
					add(path+"."+field, "%q is not a column of %s (%s)", column, p.Source, schema.names())
				} else if c.Type != Number {
					add(path+"."+field, "%q is not numeric", column)
				}
			}
		}
		if p.Total != "" && p.Viz != "stat" {
			add(path+".total", "only stat panels show a total")
		}
		if len(p.Empty) > 160 {
			add(path+".empty", "at most 160 characters")
		}
		switch p.Viz {
		case "timeseries":
			if schema.column("ts") == nil {
				add(path+".source", "timeseries needs a source with a ts column: activity")
			}
		case "bar":
			if p.By == "" {
				add(path+".by", "bar panels group by a column")
			}
		case "inbox":
			if p.Source != "inbox" {
				add(path+".source", "inbox panels show the inbox source")
			}
		case "timeline":
			if p.Source != "flow" {
				add(path+".source", "timeline panels show the flow source")
			}
		}
	}
	if len(problems) > 0 {
		return problems
	}
	return nil
}

func parseMeasure(m string) (fn, column string, err error) {
	if m == "count" {
		return "count", "", nil
	}
	fn, column, ok := strings.Cut(m, ":")
	if !ok || column == "" || !slices.Contains([]string{"sum", "avg", "max", "min"}, fn) {
		return "", "", fmt.Errorf("must be count, or sum:, avg:, max: or min: followed by a column")
	}
	return fn, column, nil
}
