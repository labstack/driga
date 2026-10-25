package dashboard

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/labstack/driga/internal/panel"
)

// maxOperations bounds one edit.
const maxOperations = 40

// Operation is one typed change to a dashboard.
type Operation struct {
	Op        string           `json:"op" jsonschema:"add, update, remove, move, layout, variables or rename"`
	ID        string           `json:"id,omitempty" jsonschema:"update, remove and move: the panel's id"`
	Panel     *panel.Panel     `json:"panel,omitempty" jsonschema:"add: the new panel; update: fields to change, merged over the panel"`
	Before    string           `json:"before,omitempty" jsonschema:"add and move: place before this panel id; default the end"`
	Grids     map[string]Grid  `json:"grids,omitempty" jsonschema:"layout: grid positions by panel id, as the app sends after a drag"`
	Variables []panel.Variable `json:"variables,omitempty" jsonschema:"variables: the dashboard's full variable list"`
	Name      string           `json:"name,omitempty" jsonschema:"rename: the new name"`
}

// Grid is a panel position on the 12-column grid.
type Grid = panel.Grid

// Apply returns current with ops applied in order, or the first failure.
// The result is validated on save.
func Apply(current panel.Dashboard, ops []Operation) (panel.Dashboard, error) {
	if len(ops) == 0 {
		return current, fmt.Errorf("no operations")
	}
	if len(ops) > maxOperations {
		return current, fmt.Errorf("at most %d operations per edit", maxOperations)
	}
	d := clone(current)
	index := func(id string) int { return slices.IndexFunc(d.Panels, func(p panel.Panel) bool { return p.ID == id }) }
	for i, op := range ops {
		fail := func(format string, args ...any) (panel.Dashboard, error) {
			return current, fmt.Errorf("operations[%d] %s: %s", i, op.Op, fmt.Sprintf(format, args...))
		}
		switch op.Op {
		case "add":
			if op.Panel == nil {
				return fail("needs a panel")
			}
			if index(op.Panel.ID) >= 0 {
				return fail("panel %q already exists", op.Panel.ID)
			}
			at := len(d.Panels)
			if op.Before != "" {
				if at = index(op.Before); at < 0 {
					return fail("no panel %q to place before", op.Before)
				}
			}
			p := *op.Panel
			p.Grid = nil
			d.Panels = slices.Insert(d.Panels, at, p)
		case "update":
			at := index(op.ID)
			if at < 0 || op.Panel == nil {
				return fail("needs an existing panel id and the fields to change")
			}
			merged, err := merge(d.Panels[at], *op.Panel)
			if err != nil {
				return fail("%s", err)
			}
			merged.ID = d.Panels[at].ID
			d.Panels[at] = merged
		case "remove":
			at := index(op.ID)
			if at < 0 {
				return fail("no panel %q", op.ID)
			}
			d.Panels = slices.Delete(d.Panels, at, at+1)
		case "move":
			at := index(op.ID)
			if at < 0 {
				return fail("no panel %q", op.ID)
			}
			p := d.Panels[at]
			d.Panels = slices.Delete(d.Panels, at, at+1)
			to := len(d.Panels)
			if op.Before != "" {
				if to = index(op.Before); to < 0 {
					return fail("no panel %q to place before", op.Before)
				}
			}
			d.Panels = slices.Insert(d.Panels, to, p)
		case "layout":
			for id, g := range op.Grids {
				at := index(id)
				if at < 0 {
					return fail("no panel %q", id)
				}
				if g.W < 1 || g.W > 12 || g.X < 0 || g.X+g.W > 12 || g.Y < 0 || g.H < 1 || g.H > 40 {
					return fail("grid for %q is outside the 12-column board", id)
				}
				grid := g
				d.Panels[at].Grid = &grid
			}
		case "variables":
			d.Variables = slices.Clone(op.Variables)
		case "rename":
			d.Name = op.Name
		default:
			return fail("unknown operation")
		}
	}
	return d, nil
}

// merge overlays the fields set in patch onto p.
func merge(p, patch panel.Panel) (panel.Panel, error) {
	base, err := json.Marshal(p)
	if err != nil {
		return p, err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(base, &fields); err != nil {
		return p, err
	}
	over, err := json.Marshal(patch)
	if err != nil {
		return p, err
	}
	changes := map[string]json.RawMessage{}
	if err := json.Unmarshal(over, &changes); err != nil {
		return p, err
	}
	// A typed patch carries every field; only the ones it sets change.
	for k, v := range changes {
		switch string(v) {
		case `""`, "0", "null", "false", "[]":
			continue
		}
		fields[k] = v
	}
	b, err := json.Marshal(fields)
	if err != nil {
		return p, err
	}
	var out panel.Panel
	return out, json.Unmarshal(b, &out)
}
