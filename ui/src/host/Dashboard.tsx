import { useEffect, useRef, useState } from "react";
import { GridLayout, useContainerWidth, type Layout } from "react-grid-layout";
import "react-grid-layout/css/styles.css";
import { ActionIcon, Box, Group, Text, Tooltip } from "@mantine/core";
import { CheckCircle, X } from "@phosphor-icons/react";
import { PanelView, type Act } from "../panels/PanelView";
import type { Fragment, Panel, Result } from "../panels/types";

const rowHeight = 36;
const rowsFor = { s: 2, m: 7, l: 12 } as const;
const widthFor: Record<Panel["viz"], number> = { stat: 3, gauge: 3, timeseries: 6, bar: 6, table: 12, list: 6, inbox: 8, cards: 12, timeline: 12, text: 6 };

type Item = { i: string; x: number; y: number; w: number; h: number };

function size(p: Panel) {
  const w = Math.min(12, p.width ?? widthFor[p.viz]);
  const h = rowsFor[p.height ?? (p.viz === "stat" || p.viz === "gauge" ? "s" : p.viz === "inbox" ? "l" : "m")];
  return { w, h };
}

const gap = 12;
const unit = rowHeight + gap;
const chrome = 58; // a panel's title and padding

// fitted is the rows a list-like panel needs for what it shows now, so a
// short list takes a short panel. Charts and text keep their size.
function fitted(p: Panel, r: Result | undefined, w: number, width: number): number | undefined {
  if (r?.status === "empty" || (r?.frame && r.frame.rows.length === 0)) return 2;
  const n = r?.frame?.rows.length;
  if (n === undefined) return undefined;
  let px: number;
  switch (p.viz) {
    case "table": px = chrome + 32 + 31 * n; break;
    case "list": px = chrome + 33 * n; break;
    case "inbox": px = chrome + 28 * 2 + 60 * n; break;
    case "cards": { const across = Math.max(1, Math.floor((width * w / 12) / 230)); px = chrome + 160 * Math.ceil(n / across); break; }
    case "stat": case "gauge": return 2;
    default: return undefined;
  }
  return Math.max(2, Math.ceil((px + gap) / unit));
}

/** layoutOf places saved grids first, then flows the rest in order into the
 * first gap that fits, so a new dashboard reads top to bottom by priority. */
export function layoutOf(panels: Panel[]): Item[] {
  const placed: Item[] = [];
  const free = (x: number, y: number, w: number, h: number) => placed.every(p => x + w <= p.x || p.x + p.w <= x || y + h <= p.y || p.y + p.h <= y);
  for (const p of panels) if (p.grid) placed.push({ i: p.id, ...p.grid });
  for (const p of panels) {
    if (p.grid) continue;
    const { w, h } = size(p);
    let spot: { x: number; y: number } | undefined;
    for (let y = 0; !spot; y++) for (let x = 0; x + w <= 12 && !spot; x++) if (free(x, y, w, h)) spot = { x, y };
    placed.push({ i: p.id, ...spot, w, h });
  }
  return placed;
}

/** packed leaves no blank space: list-like panels take the rows their
 * content needs; every panel moves up as far as it can;
 * when that is taller than the window the tallest panels give up rows and
 * scroll inside; then each panel widens into empty columns beside it and
 * grows into empty rows below it, to the window's edge. */
export function packed(layout: Item[], panels: Map<string, Panel>, results: Map<string, Result>, width: number, rows: number): Item[] {
  // A panel takes what its content needs, even past its size; overflow is
  // settled below, tallest first.
  const items = layout.map(l => ({ ...l, h: Math.min(Math.max(l.h, 12), fitted(panels.get(l.i)!, results.get(l.i), l.w, width) ?? l.h) }));
  const overlaps = (a: Item, b: Item) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
  const clear = (it: Item, at: Partial<Item>) => !items.some(d => d !== it && overlaps({ ...it, ...at }, d));
  const compact = () => {
    for (const it of [...items].sort((a, b) => a.y - b.y || a.x - b.x)) {
      while (it.y > 0 && items.every(d => d === it || d.y > it.y || !overlaps({ ...it, y: it.y - 1 }, d))) it.y--;
    }
  };
  const bottom = () => Math.max(0, ...items.map(i => i.y + i.h));
  compact();
  while (bottom() > rows) {
    const tallest = items.filter(i => i.h > 3).sort((a, b) => b.h - a.h)[0];
    if (!tallest) break;
    tallest.h--;
    compact();
  }
  for (const it of items) {
    while (it.x > 0 && clear(it, { x: it.x - 1, w: 1 })) { it.x--; it.w++; }
    while (it.x + it.w < 12 && clear(it, { x: it.x + it.w, w: 1 })) it.w++;
  }
  const edge = Math.max(rows, bottom());
  for (const it of [...items].sort((a, b) => a.y - b.y)) {
    while (it.y + it.h < edge && clear(it, { y: it.y + it.h, h: 1 })) it.h++;
  }
  return items;
}

// useHeight follows the height of the board's container; key changes when
// the board element is replaced.
function useHeight(key: boolean) {
  const ref = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState(0);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const observe = new ResizeObserver(() => setHeight(el.clientHeight));
    observe.observe(el);
    return () => observe.disconnect();
  }, [key]);
  return { ref, height };
}

/** Dashboard is the app's board, packed to fill its space. Editing drags and
 * resizes panels on the same grid; a move is saved as one layout edit when
 * the drag ends. */
export function Dashboard({ fragment, editing, act, onLayout, onRemove }: {
  fragment: Fragment; editing: boolean; act?: Act;
  onLayout: (grids: Record<string, { x: number; y: number; w: number; h: number }>) => void;
  onRemove: (id: string) => void;
}) {
  const { width, containerRef, mounted } = useContainerWidth();
  const fill = useHeight(editing);
  const results = new Map(fragment.results.map(r => [r.id, r]));
  const byId = new Map(fragment.dashboard.panels.map(p => [p.id, p]));
  // Editing starts from the packed board the user sees, so nothing jumps;
  // a drag saves every panel where it now is.
  // Viewed, a panel with nothing to show leaves the grid for the all-clear
  // strip, and the panels with something to show take the space.
  const clear = editing ? [] : fragment.dashboard.panels.filter(p => results.get(p.id)?.status === "empty");
  const gridPanels = editing ? fragment.dashboard.panels : fragment.dashboard.panels.filter(p => !clear.includes(p));
  const available = Math.max(1, Math.floor((fill.height + gap) / unit));
  const items = packed(layoutOf(gridPanels), byId, results, fill.ref.current?.clientWidth ?? 1200, available);
  const ref = (el: HTMLDivElement | null) => { fill.ref.current = el; containerRef.current = el; };
  if (!editing) {
    const rows = Math.max(1, ...items.map(i => i.y + i.h));
    return <>
      {clear.length > 0 && <AllClear panels={clear} results={results} />}
      {/* Rows share any height left after whole rows, so the board ends exactly at the edge. */}
      <Box ref={ref} style={{ flex: 1, minHeight: 0, display: "grid", gridTemplateColumns: "repeat(12, minmax(0, 1fr))", gap, gridTemplateRows: `repeat(${rows}, minmax(${rowHeight}px, 1fr))` }}>
        {items.map(it => <Box key={it.i} style={{ minWidth: 0, minHeight: 0, gridColumn: `${it.x + 1} / span ${it.w}`, gridRow: `${it.y + 1} / span ${it.h}` }}>
          <PanelView panel={byId.get(it.i)!} result={results.get(it.i)} act={act} fill />
        </Box>)}
      </Box>
    </>;
  }
  const save = (next: Layout) => {
    const changed = next.some(n => { const before = items.find(l => l.i === n.i); return !before || before.x !== n.x || before.y !== n.y || before.w !== n.w || before.h !== n.h; });
    if (changed) onLayout(Object.fromEntries(next.map(n => [n.i, { x: n.x, y: n.y, w: n.w, h: n.h }])));
  };
  return <Box ref={ref} className="editing" style={{ flex: 1, minHeight: 0, overflow: "auto" }}>
    {mounted && <GridLayout width={width} layout={items} gridConfig={{ cols: 12, rowHeight, margin: [gap, gap], containerPadding: [0, 0] }}
      dragConfig={{ enabled: true, handle: ".panel-handle" }} resizeConfig={{ enabled: true }}
      onDragStop={(next: Layout) => save(next)} onResizeStop={(next: Layout) => save(next)}>
      {items.map(it => <div key={it.i} style={{ position: "relative" }}>
        <PanelView panel={byId.get(it.i)!} result={results.get(it.i)} fill />
        <ActionIcon variant="default" pos="absolute" top={8} right={8} aria-label={`Remove ${byId.get(it.i)!.title}`} onClick={() => onRemove(it.i)}><X /></ActionIcon>
      </div>)}
    </GridLayout>}
  </Box>;
}

/** AllClear names the panels with nothing to show, one line for all of them:
 * each title with a check, and on hover what would appear there. */
function AllClear({ panels, results }: { panels: Panel[]; results: Map<string, Result> }) {
  return <Group gap="lg" mb={8} px={2} aria-label="Nothing to show">
    {panels.map(p => <Tooltip key={p.id} label={results.get(p.id)?.message ?? p.empty ?? "Nothing matches right now."} multiline maw={300}>
      <Group gap={6} wrap="nowrap" c="dimmed">
        <CheckCircle aria-hidden />
        <Text size="sm" id={`panel-${p.id}`}>{p.title}: <Text span c="dimmed" size="sm">none</Text></Text>
      </Group>
    </Tooltip>)}
  </Group>;
}
