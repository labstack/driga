package view

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// BoardMap is a deterministic, server-rendered slot layout. Coordinates depend
// exclusively on session membership, project, parent identity and start time.
type BoardMap struct {
	Regions []BoardRegion
	Nodes   []BoardMapNode
	Edges   []BoardEdge
	Badges  []BoardBadge
}
type BoardRegion struct {
	W, H, LX, LY, RX int
	Label, Sub, Dash string
}
type BoardMapNode struct {
	BoardRow
	Region, X, Y, GY, LX, LY, MY int
	Fill, Opacity                float64
	Dash, Label, Meta, Aria      string
}
type BoardEdge struct {
	Path, Kind, File string
	A, B             string // overlap endpoints, shown while either is selected
	Region           int
}
type BoardBadge struct {
	Region, X, Y, TX, TY int
	Text, Key            string
}

func BoardGlyph(state string) string {
	if state == "finished" {
		return "✓"
	}
	return FlowGlyph(state)
}
func BoardAge(r BoardRow, now time.Time) string {
	at := r.Node.StartedAt
	if r.State == "waiting" {
		at = r.Node.LastActivityAt
		if r.Wait != nil {
			at = r.Wait.StartedAt
		}
	}
	if r.State == "stuck" {
		at = r.Node.LastActivityAt
	}
	if at.IsZero() {
		return ""
	}
	d := max(time.Duration(0), now.Sub(at))
	if d >= time.Hour {
		return fmt.Sprintf("%dh %dm", int(d/time.Hour), int(d/time.Minute)%60)
	}
	if d >= time.Minute {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return fmt.Sprintf("%ds", int(d/time.Second))
}
func boardFileName(path string) string {
	parts := strings.Split(strings.ReplaceAll(path, "\\", "/"), "/")
	return parts[len(parts)-1]
}
func (b *BoardView) buildMap(remote map[string]bool) {
	b.Map = BoardMap{}
	positions := map[string]BoardMapNode{}
	// Shared paths are evidence only within the same machine. Basenames never
	// establish overlap. Ended sessions do not imply a current file conflict.
	files := map[string][]string{}
	for gi := range b.Groups {
		for ri := range b.Groups[gi].Rows {
			r := &b.Groups[gi].Rows[ri]
			if r.State == "finished" {
				continue
			}
			for _, f := range r.Files {
				key := r.Node.MachineID + "\x00" + f.Path
				files[key] = append(files[key], r.Key)
			}
		}
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	overlaps := map[string][]string{}
	for _, path := range paths {
		keys := files[path]
		if len(keys) < 2 {
			continue
		}
		name := boardFileName(strings.SplitN(path, "\x00", 2)[1])
		for _, k := range keys {
			overlaps[k] = append(overlaps[k], name)
		}
	}
	// Each family is a compact block: the lead on top, its workers indented
	// below it, one row each.
	type block struct {
		rows []BoardRow
		fold int
	}
	type pending struct {
		region BoardRegion
		blocks []block
	}
	var layout []pending
	for gi := range b.Groups {
		g := &b.Groups[gi]
		machines := []string{}
		byMachine := map[string][]BoardRow{}
		for ri := range g.Rows {
			r := &g.Rows[ri]
			r.Overlaps = overlaps[r.Key]
			if _, ok := byMachine[r.Node.MachineID]; !ok {
				machines = append(machines, r.Node.MachineID)
			}
			byMachine[r.Node.MachineID] = append(byMachine[r.Node.MachineID], *r)
		}
		sort.Strings(machines)
		for _, machine := range machines {
			families := map[string][]BoardRow{}
			roots := []string{}
			for _, r := range byMachine[machine] {
				if _, ok := families[r.Root]; !ok {
					roots = append(roots, r.Root)
				}
				families[r.Root] = append(families[r.Root], r)
			}
			p := pending{region: BoardRegion{Label: g.Name, Sub: strings.Join(g.Branches, ", ")}}
			if remote[machine] {
				p.region.Dash = "6 6"
				p.region.Sub = "⇄ " + machine
			}
			for _, root := range roots {
				f := families[root]
				blk := block{rows: f[:min(len(f), boardMapWorkers+1)]}
				blk.fold = len(f) - len(blk.rows)
				p.blocks = append(p.blocks, blk)
			}
			layout = append(layout, p)
		}
	}
	blockHeight := func(blk block) int {
		n := len(blk.rows)
		if blk.fold > 0 {
			n++
		}
		return n * boardMapRow
	}
	// Each region is its own SVG in local coordinates, one family under the
	// next. The page flows regions into columns, so the map fits any width.
	for ri, p := range layout {
		w := boardMapBlock + 2*boardMapPad
		h := boardMapHead
		for _, blk := range p.blocks {
			for j, r := range blk.rows {
				nx, ny := boardMapPad+boardMapRadius+4, h+j*boardMapRow+boardMapRow/2
				if j > 0 {
					nx += boardMapIndent
				}
				n := boardMapNode(r, nx, ny)
				n.Region = ri
				b.Map.Nodes = append(b.Map.Nodes, n)
				positions[r.Key] = n
			}
			if blk.fold > 0 {
				fy := h + len(blk.rows)*boardMapRow + boardMapRow/2
				fx := boardMapPad + boardMapRadius + 4 + boardMapIndent
				b.Map.Badges = append(b.Map.Badges, BoardBadge{Region: ri, X: fx - boardMapRadius, Y: fy - 10, TX: fx - boardMapRadius + 20, TY: fy + 4, Text: fmt.Sprintf("+%d", blk.fold), Key: blk.rows[0].Root})
			}
			h += blockHeight(blk)
		}
		h += boardMapPad / 2
		p.region.W, p.region.H = w, h
		p.region.LX, p.region.LY, p.region.RX = boardMapPad, 28, w-boardMapPad
		b.Map.Regions = append(b.Map.Regions, p.region)
	}
	for _, n := range b.Map.Nodes {
		if p, ok := positions[n.Parent]; ok {
			b.Map.Edges = append(b.Map.Edges, BoardEdge{Region: n.Region, Kind: "spawn", Path: fmt.Sprintf("M %d %d V %d H %d", p.X, p.Y+boardMapRadius, n.Y, n.X-boardMapRadius)})
		}
	}
	for _, path := range paths {
		keys := files[path]
		if len(keys) < 2 {
			continue
		}
		name := boardFileName(strings.SplitN(path, "\x00", 2)[1])
		sort.Strings(keys)
		// A star per shared file avoids quadratic edges at high session counts.
		// Centre on the first visible session; folded ones have no position.
		centre := -1
		for i, k := range keys {
			if _, ok := positions[k]; ok {
				centre = i
				break
			}
		}
		if centre < 0 {
			continue
		}
		a := positions[keys[centre]]
		for _, k := range keys[centre+1:] {
			c, ok := positions[k]
			if !ok || c.Region != a.Region {
				continue // across projects the "┄ file" tag on each session says it
			}
			// Sessions in a region share one column, so a link drawn between
			// them crosses nodes and titles. A bracket in the region's left
			// margin joins them instead; each node's meta already names the file.
			b.Map.Edges = append(b.Map.Edges, BoardEdge{Region: a.Region, Kind: "overlap", File: name, A: keys[centre], B: k,
				Path: fmt.Sprintf("M %d %d H %d V %d H %d", a.X-boardMapRadius-3, a.Y, boardMapGutter, c.Y, c.X-boardMapRadius-3)})
		}
	}
	for i := range b.Needs {
		b.Needs[i].Overlaps = overlaps[b.Needs[i].Key]
	}
}

const (
	boardMapBlock   = 250 // region content width: families stack in one column
	boardMapPad     = 20  // inside a region
	boardMapGutter  = 7   // overlap brackets run down the region's left margin
	boardMapHead    = 44  // region title band
	boardMapRow     = 48  // one session row
	boardMapRadius  = 14
	boardMapIndent  = 28 // workers sit under their lead, indented
	boardMapWorkers = 4  // more fold into a +N badge
	boardMapLabel   = 24 // runes before a title is cut
)

func boardMapNode(r BoardRow, x, y int) BoardMapNode {
	fill := 0.08
	for _, s := range r.Slices[max(0, len(r.Slices)-5):] {
		if s.Activity > 0 || s.Events > 0 {
			fill += 0.064
		}
	}
	opacity, dash := 1.0, ""
	switch r.State {
	case "finished":
		opacity, dash = 0.45, "1 4"
	case "stuck":
		dash = "4 3"
	}
	age := ""
	if r.State == "waiting" || r.State == "stuck" {
		age = r.Age
	}
	label := r.Title
	if runes := []rune(label); len(runes) > boardMapLabel {
		label = string(runes[:boardMapLabel-1]) + "…"
	}
	n := BoardMapNode{BoardRow: r, X: x, Y: y, GY: y + 4, LX: x + boardMapRadius + 10, LY: y - 2, MY: y + 13, Fill: fill, Opacity: opacity, Dash: dash, Label: label,
		Meta: FlowJoin(r.Node.Adapter, age), Aria: FlowJoin(r.Title, strings.TrimSpace(r.State+" "+age), r.Node.Adapter)}
	n.Age = age
	return n
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// BoardTools counts reported tool events (OTLP telemetry or agent hooks) in the
// row's window, never transcript or CPU activity.
func BoardTools(r BoardRow) string {
	var from time.Time
	if len(r.Slices) > 0 {
		from = r.Slices[0].At
	}
	counts := map[string]int{}
	for _, e := range r.Node.Events {
		if e.TS.Before(from) || FlowEventMark(r.Node.Adapter, e.Kind) != "tool" {
			continue
		}
		name := "tool"
		if e.Metadata != nil && e.Metadata.Tool != "" {
			name = e.Metadata.Tool
		}
		counts[name]++
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	out := []string{}
	for _, name := range names {
		out = append(out, fmt.Sprintf("%s %d", name, counts[name]))
	}
	return strings.Join(out, " · ")
}

func BoardSparkPoints(buckets []int) string {
	if len(buckets) == 0 {
		return "0,20 120,20"
	}
	peak := 1
	for _, n := range buckets {
		peak = max(peak, n)
	}
	var out strings.Builder
	for i, n := range buckets {
		fmt.Fprintf(&out, "%.1f,%.1f ", float64(i)*120/float64(max(1, len(buckets)-1)), 20-float64(n)*18/float64(peak))
	}
	return strings.TrimSpace(out.String())
}
