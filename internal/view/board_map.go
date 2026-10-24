package view

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// BoardMap is a deterministic, server-rendered slot layout. Coordinates depend
// exclusively on session membership, project, parent identity and start time.
type BoardMap struct {
	Width, Height int
	Regions       []BoardRegion
	Nodes         []BoardMapNode
	Edges         []BoardEdge
	Badges        []BoardBadge
}
type BoardRegion struct {
	X, Y, W, H, LX, LY, RX int
	Label, Sub, Dash       string
}
type BoardMapNode struct {
	BoardRow
	X, Y, GY, LX, LY, MY    int
	Fill, Opacity           float64
	Dash, Label, Meta, Aria string
}
type BoardEdge struct {
	Path, Kind, File string
	A, B             string // overlap endpoints, shown while either is selected
	X, Y, W          int
}
type BoardBadge struct {
	X, Y, TX, TY int
	Text, Key    string
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
	if r.State == "stalled" {
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
	b.Map = BoardMap{Width: 1000, Height: 240}
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
	// below it, one row each. Blocks fill a region in rows of up to three, and
	// regions are packed into lanes (see below).
	type block struct {
		rows []BoardRow
		fold int
	}
	type pending struct {
		region BoardRegion
		blocks []block
		cols   int
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
			p := pending{region: BoardRegion{Label: g.Name, Sub: strings.Join(g.Branches, ", ")}, cols: min(boardMapLanes-1, len(roots))}
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
	// Regions take whole lanes and drop into the lowest gap that fits them,
	// leftmost first, so small regions fill space beside large ones.
	sky := make([]int, boardMapLanes)
	for i := range sky {
		sky[i] = boardMapGap
	}
	width := 0
	for _, p := range layout {
		lane, y := 0, -1
		for s := 0; s+p.cols <= boardMapLanes; s++ {
			top := 0
			for _, t := range sky[s : s+p.cols] {
				top = max(top, t)
			}
			if y < 0 || top < y {
				lane, y = s, top
			}
		}
		x := boardMapGap + lane*boardMapLane
		w := p.cols*boardMapLane - boardMapGap
		block := (w - 2*boardMapPad) / p.cols
		h := boardMapHead
		for i := 0; i < len(p.blocks); i += p.cols {
			rowH := 0
			for _, blk := range p.blocks[i:min(len(p.blocks), i+p.cols)] {
				rowH = max(rowH, blockHeight(blk))
			}
			for c, blk := range p.blocks[i:min(len(p.blocks), i+p.cols)] {
				bx, by := x+boardMapPad+c*block, y+h
				for j, r := range blk.rows {
					nx, ny := bx+boardMapRadius+4, by+j*boardMapRow+boardMapRow/2
					if j > 0 {
						nx += boardMapIndent
					}
					n := boardMapNode(r, nx, ny)
					b.Map.Nodes = append(b.Map.Nodes, n)
					positions[r.Key] = n
				}
				if blk.fold > 0 {
					fy := by + len(blk.rows)*boardMapRow + boardMapRow/2
					fx := bx + boardMapRadius + 4 + boardMapIndent
					b.Map.Badges = append(b.Map.Badges, BoardBadge{X: fx - boardMapRadius, Y: fy - 10, TX: fx - boardMapRadius + 20, TY: fy + 4, Text: fmt.Sprintf("+%d", blk.fold), Key: blk.rows[0].Root})
				}
			}
			h += rowH
		}
		h += boardMapPad / 2
		p.region.X, p.region.Y, p.region.W, p.region.H = x, y, w, h
		p.region.LX, p.region.LY, p.region.RX = x+boardMapPad, y+28, x+w-boardMapPad
		b.Map.Regions = append(b.Map.Regions, p.region)
		for i := lane; i < lane+p.cols; i++ {
			sky[i] = y + h + boardMapGap
		}
		width = max(width, x+w+boardMapGap)
	}
	b.Map.Width, b.Map.Height = max(320, width), max(160, slices.Max(sky))
	for _, n := range b.Map.Nodes {
		if p, ok := positions[n.Parent]; ok {
			b.Map.Edges = append(b.Map.Edges, BoardEdge{Kind: "spawn", Path: fmt.Sprintf("M %d %d V %d H %d", p.X, p.Y+boardMapRadius, n.Y, n.X-boardMapRadius)})
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
		a, ok := positions[keys[0]]
		if !ok {
			continue
		}
		for _, k := range keys[1:] {
			c, ok := positions[k]
			if !ok {
				continue
			}
			b.Map.Edges = append(b.Map.Edges, BoardEdge{Kind: "overlap", File: name, A: keys[0], B: k, W: len([]rune(name))*7 + 16, X: (a.X+c.X)/2 - (len([]rune(name))*7+16)/2, Y: min(a.Y, c.Y) - boardMapRow/2 - 10 + abs(a.Y-c.Y)/4, Path: fmt.Sprintf("M %d %d Q %d %d %d %d", a.X, a.Y, (a.X+c.X)/2, min(a.Y, c.Y)-boardMapRow, c.X, c.Y)})
		}
	}
	for i := range b.Needs {
		b.Needs[i].Overlaps = overlaps[b.Needs[i].Key]
	}
}

const (
	boardMapLanes   = 4   // map columns; a region spans one to three
	boardMapLane    = 260 // lane width including the gap
	boardMapGap     = 20  // between regions
	boardMapPad     = 20  // inside a region
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
	case "stalled":
		dash = "4 3"
	}
	age := ""
	if r.State == "waiting" || r.State == "stalled" {
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

// BoardTools counts only agent tool telemetry, never transcript or CPU activity.
func BoardTools(r BoardRow) string {
	counts := map[string]int{}
	for _, e := range r.Node.Events {
		if FlowEventMark(r.Node.Adapter, e.Kind) != "tool" {
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
