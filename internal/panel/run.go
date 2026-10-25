package panel

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/driga/internal/viewer"
)

// Result statuses.
const (
	StatusOK    = "ok"
	StatusEmpty = "empty"
	StatusError = "error"
)

const defaultLimit = 50

// identity are the columns actions need to name a row: its id, session and
// machine, plus what decides whether an action is offered.
var identity = []string{"id", "ids", "session_id", "machine_id", "path", "verified", "young", "status", "label", "state", "inferred", "evidence", "kind"}

// RunRequest asks for a dashboard's panels with variable values. Panels, when
// set, limits the run to those ids.
type RunRequest struct {
	Dashboard Dashboard         `json:"dashboard"`
	Vars      map[string]string `json:"vars,omitempty" jsonschema:"Variable values by name; empty means all"`
	Panels    []string          `json:"panels,omitempty" jsonschema:"Panel ids to run; default all"`
}

// Result is one panel's data.
type Result struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Frame   *Frame `json:"frame,omitempty"`
	// Trend is a stat's value per minute, oldest first, when its source has
	// readings over time; missing minutes are null.
	Trend []any `json:"trend,omitempty"`
	// Total is what a stat is out of, when its panel names one.
	Total any `json:"total,omitempty"`
	// Title is the panel's title with its $variables filled in, when it has any.
	Title string `json:"title,omitempty"`
}

// Fragment is a dashboard with its results: what a tool returns and an MCP
// App or the app's dashboard renders.
type Fragment struct {
	Dashboard  Dashboard           `json:"dashboard"`
	Results    []Result            `json:"results"`
	Vars       map[string]string   `json:"vars,omitempty"`
	Options    map[string][]string `json:"options,omitempty"`
	ObservedAt string              `json:"observed_at"`
	ID         string              `json:"id,omitempty"`
	Version    int                 `json:"version,omitempty"`
	// LocalMachine is the machine the app runs on; actions act only there.
	LocalMachine string `json:"local_machine"`
}

// Run validates the dashboard and runs its panels over snap.
func Run(snap viewer.Snapshot, req RunRequest) (Fragment, error) {
	if err := Validate(req.Dashboard); err != nil {
		return Fragment{}, err
	}
	vars := map[string]string{}
	for _, v := range req.Dashboard.Variables {
		vars[v.Name] = v.Default
		if value, ok := req.Vars[v.Name]; ok {
			vars[v.Name] = value
		}
	}
	base := map[string][][]any{}
	rows := func(source string) [][]any {
		if r, ok := base[source]; ok {
			return r
		}
		r := sources[source].build(snap)
		base[source] = r
		return r
	}
	f := Fragment{Dashboard: req.Dashboard, Results: []Result{}, Vars: vars, Options: options(req.Dashboard, rows), ObservedAt: snap.ObservedAt.UTC().Format(time.RFC3339), LocalMachine: snap.LocalMachineID}
	for _, p := range req.Dashboard.Panels {
		if len(req.Panels) > 0 && !slices.Contains(req.Panels, p.ID) {
			continue
		}
		f.Results = append(f.Results, runPanel(p, vars, rows))
	}
	return f, nil
}

func options(d Dashboard, rows func(string) [][]any) map[string][]string {
	if len(d.Variables) == 0 {
		return nil
	}
	out := map[string][]string{}
	for _, v := range d.Variables {
		seen := map[string]bool{}
		column := variableColumns[v.Field]
		for _, name := range SourceNames() {
			s := sources[name]
			i := slices.IndexFunc(s.columns, func(c Column) bool { return c.Name == column })
			if i < 0 {
				continue
			}
			for _, r := range rows(name) {
				if value, ok := r[i].(string); ok && value != "" {
					seen[value] = true
				}
			}
		}
		values := make([]string, 0, len(seen))
		for value := range seen {
			values = append(values, value)
		}
		slices.Sort(values)
		out[v.Name] = values
	}
	return out
}

func runPanel(p Panel, vars map[string]string, rows func(string) [][]any) Result {
	r := Result{ID: p.ID, Status: StatusOK, Title: fill(p.Title, vars)}
	if p.Viz == "text" {
		return r
	}
	s := sources[p.Source]
	data := filter(s, rows(p.Source), p.Where, vars)
	var frame Frame
	switch {
	case p.Viz == "stat" || p.Viz == "gauge":
		value, unit := aggregate(s, data, p.Measure)
		if slices.ContainsFunc(s.columns, func(c Column) bool { return c.Name == "ts" }) {
			// Over time, the stat is the latest reading and the trend leads to it.
			for _, row := range timeseries(s, data, p).Rows {
				r.Trend = append(r.Trend, row[2])
				if row[2] != nil {
					value = row[2]
				}
			}
		}
		frame = Frame{Columns: []Column{{Name: "value", Type: Number, Unit: cmp.Or(p.Unit, unit)}}, Rows: [][]any{{value}}}
		if p.Total != "" {
			r.Total, _ = aggregate(s, data, p.Total)
		}
	case p.Viz == "timeseries":
		frame = timeseries(s, data, p)
	case p.Viz == "timeline":
		frame = timeline(s, data, p, rows("waits"))
	case p.By != "":
		frame = grouped(s, data, p)
	default:
		frame = project(s, data, p)
	}
	if len(frame.Rows) == 0 {
		// An empty frame is [] on the wire, never null.
		frame.Rows = [][]any{}
		r.Status, r.Message = StatusEmpty, cmp.Or(p.Empty, emptyFor(p.Viz))
	}
	r.Frame = &frame
	return r
}

func filter(s schema, rows [][]any, where []Filter, vars map[string]string) [][]any {
	if len(where) == 0 {
		return rows
	}
	out := make([][]any, 0, len(rows))
	for _, r := range rows {
		keep := true
		for _, f := range where {
			value := f.Value
			if name, ok := strings.CutPrefix(value, "$"); ok {
				value = vars[name]
				if value == "" {
					continue // An empty variable means all.
				}
			}
			i := slices.IndexFunc(s.columns, func(c Column) bool { return c.Name == f.Column })
			if !match(r[i], f.Op, value) {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, r)
		}
	}
	return out
}

// emptyFor is what a panel with no rows says when its spec says nothing.
func emptyFor(viz string) string {
	if viz == "inbox" {
		return "Nothing needs you right now."
	}
	return "Nothing matches right now."
}

// fill writes each $variable in text as its value, or as "all <name>s" when
// it has none, which is what an empty variable selects.
func fill(text string, vars map[string]string) string {
	if !strings.Contains(text, "$") {
		return ""
	}
	// Longer names first, so $project_path is not read as $project.
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	for _, name := range names {
		text = strings.ReplaceAll(text, "$"+name, cmp.Or(vars[name], "all "+name+"s"))
	}
	return text
}

func match(cell any, op, value string) bool {
	text := fmt.Sprint(cell)
	if cell == nil {
		text = ""
	}
	switch op {
	case "", "eq":
		return text == value
	case "ne":
		return text != value
	case "in":
		return slices.Contains(strings.Split(value, ","), text)
	case "contains":
		return strings.Contains(strings.ToLower(text), strings.ToLower(value))
	case "gt", "lt":
		n, ok := cell.(float64)
		v, err := strconv.ParseFloat(value, 64)
		if !ok || err != nil {
			// Times compare as times: occurred_at gt 2026-10-25T00:00:00Z.
			at, errA := time.Parse(time.RFC3339, text)
			than, errB := time.Parse(time.RFC3339, value)
			if errA != nil || errB != nil {
				return false
			}
			if op == "gt" {
				return at.After(than)
			}
			return at.Before(than)
		}
		if op == "gt" {
			return n > v
		}
		return n < v
	}
	return false
}

// aggregate reduces rows to one number with measure (default count). With
// no readings to sum, it returns nil: missing data is not zero.
func aggregate(s schema, rows [][]any, measure string) (any, string) {
	fn, column, _ := parseMeasure(cmp.Or(measure, "count"))
	if fn == "count" {
		return float64(len(rows)), "count"
	}
	i := slices.IndexFunc(s.columns, func(c Column) bool { return c.Name == column })
	var values []float64
	for _, r := range rows {
		if n, ok := r[i].(float64); ok && !math.IsNaN(n) {
			values = append(values, n)
		}
	}
	unit := s.columns[i].Unit
	// Nothing to add up is zero; rows without readings are no reading.
	if len(rows) == 0 && fn == "sum" {
		return 0.0, unit
	}
	if len(values) == 0 {
		return nil, unit
	}
	switch fn {
	case "sum", "avg":
		total := 0.0
		for _, n := range values {
			total += n
		}
		if fn == "avg" {
			total /= float64(len(values))
		}
		return total, unit
	case "max":
		return slices.Max(values), unit
	default:
		return slices.Min(values), unit
	}
}

func grouped(s schema, rows [][]any, p Panel) Frame {
	i := slices.IndexFunc(s.columns, func(c Column) bool { return c.Name == p.By })
	groups := map[string][][]any{}
	var keys []string
	for _, r := range rows {
		key := fmt.Sprint(r[i])
		if r[i] == nil {
			key = ""
		}
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], r)
	}
	_, unit := aggregate(s, nil, p.Measure)
	frame := Frame{Columns: []Column{{Name: p.By, Type: String}, {Name: "value", Type: Number, Unit: cmp.Or(p.Unit, unit)}}}
	for _, key := range keys {
		value, _ := aggregate(s, groups[key], p.Measure)
		frame.Rows = append(frame.Rows, []any{key, value})
	}
	sortRows(frame, cmp.Or(p.Sort, "-value"))
	return limit(frame, p.Limit)
}

func timeseries(s schema, rows [][]any, p Panel) Frame {
	ts := slices.IndexFunc(s.columns, func(c Column) bool { return c.Name == "ts" })
	by := -1
	if p.By != "" {
		by = slices.IndexFunc(s.columns, func(c Column) bool { return c.Name == p.By })
	}
	type key struct{ ts, series string }
	groups := map[key][][]any{}
	var order []key
	for _, r := range rows {
		k := key{ts: fmt.Sprint(r[ts])}
		if by >= 0 {
			k.series = fmt.Sprint(r[by])
		}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}
	_, unit := aggregate(s, nil, p.Measure)
	frame := Frame{Columns: []Column{{Name: "ts", Type: Time}, {Name: "series", Type: String}, {Name: "value", Type: Number, Unit: cmp.Or(p.Unit, unit)}}}
	for _, k := range order {
		value, _ := aggregate(s, groups[k], p.Measure)
		frame.Rows = append(frame.Rows, []any{k.ts, k.series, value})
	}
	slices.SortStableFunc(frame.Rows, func(a, b []any) int { return cmp.Compare(a[0].(string), b[0].(string)) })
	return frame
}

func project(s schema, rows [][]any, p Panel) Frame {
	names := p.Columns
	if len(names) == 0 {
		for _, c := range s.columns {
			names = append(names, c.Name)
		}
	}
	frame := Frame{}
	// A row keeps its identity even when the view leaves those columns out,
	// so the app can act on it.
	hidden := map[string]bool{}
	for _, key := range identity {
		if !slices.Contains(names, key) && slices.ContainsFunc(s.columns, func(c Column) bool { return c.Name == key }) {
			names = append(slices.Clone(names), key)
			hidden[key] = true
		}
	}
	index := make([]int, len(names))
	for j, name := range names {
		index[j] = slices.IndexFunc(s.columns, func(c Column) bool { return c.Name == name })
		c := s.columns[index[j]]
		c.Hidden = hidden[name]
		frame.Columns = append(frame.Columns, c)
	}
	// Sort on the full row so a column left out of the view still orders it.
	sorted := slices.Clone(rows)
	if i := slices.IndexFunc(s.columns, func(c Column) bool { return c.Name == strings.TrimPrefix(p.Sort, "-") }); i >= 0 {
		desc := strings.HasPrefix(p.Sort, "-")
		slices.SortStableFunc(sorted, func(a, b []any) int { return compareCells(a[i], b[i], desc) })
	} else if p.Viz == "inbox" {
		sortInbox(s, sorted)
	}
	for _, r := range sorted {
		out := make([]any, len(index))
		for j, i := range index {
			out[j] = r[i]
		}
		frame.Rows = append(frame.Rows, out)
	}
	return limit(frame, p.Limit)
}

// sortInbox puts waits first, longest waiting first, then the newest
// completions, matching the app's inbox.
func sortInbox(s schema, rows [][]any) {
	kind := slices.IndexFunc(s.columns, func(c Column) bool { return c.Name == "kind" })
	waited := slices.IndexFunc(s.columns, func(c Column) bool { return c.Name == "waited" })
	age := slices.IndexFunc(s.columns, func(c Column) bool { return c.Name == "age" })
	slices.SortStableFunc(rows, func(a, b []any) int {
		aw, bw := a[kind] == "waiting", b[kind] == "waiting"
		if aw != bw {
			if aw {
				return -1
			}
			return 1
		}
		if aw {
			return compareCells(a[waited], b[waited], true)
		}
		return compareCells(a[age], b[age], false)
	})
}

func sortRows(f Frame, sortBy string) {
	desc := strings.HasPrefix(sortBy, "-")
	name := strings.TrimPrefix(sortBy, "-")
	i := slices.IndexFunc(f.Columns, func(c Column) bool { return c.Name == name })
	if i < 0 {
		return
	}
	slices.SortStableFunc(f.Rows, func(a, b []any) int { return compareCells(a[i], b[i], desc) })
}

func compareCells(a, b any, desc bool) int {
	var c int
	switch x := a.(type) {
	case float64:
		y, _ := b.(float64)
		c = cmp.Compare(x, y)
	case bool:
		y, _ := b.(bool)
		c = cmp.Compare(boolInt(x), boolInt(y))
	default:
		c = cmp.Compare(fmt.Sprint(a), fmt.Sprint(b))
	}
	if desc {
		return -c
	}
	return c
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func limit(f Frame, n int) Frame {
	if n <= 0 {
		n = defaultLimit
	}
	if len(f.Rows) > n {
		f.Rows = f.Rows[:n]
	}
	return f
}

// timeline lists each session, newest activity first, with its waits as
// spans the renderer draws over the session's bar.
func timeline(s schema, data [][]any, p Panel, waits [][]any) Frame {
	if p.Sort == "" {
		p.Sort = "-last_activity_at"
	}
	if len(p.Columns) == 0 {
		p.Columns = []string{"id", "title", "label", "agent", "project", "started_at", "ended_at", "last_activity_at"}
	}
	for _, need := range []string{"id", "started_at", "ended_at", "last_activity_at"} {
		if !slices.Contains(p.Columns, need) {
			p.Columns = append(p.Columns, need)
		}
	}
	frame := project(s, data, p)
	shown := map[float64]bool{}
	id := slices.IndexFunc(frame.Columns, func(c Column) bool { return c.Name == "id" })
	for _, r := range frame.Rows {
		if n, ok := r[id].(float64); ok {
			shown[n] = true
		}
	}
	for _, w := range waits {
		if n, ok := w[0].(float64); ok && shown[n] {
			frame.Spans = append(frame.Spans, Span{SessionID: n, Kind: "waiting", From: w[6], To: w[7], Reason: fmt.Sprint(w[4])})
		}
	}
	return frame
}
