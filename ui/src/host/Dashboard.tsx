import { GridLayout, useContainerWidth, type Layout } from "react-grid-layout";
import "react-grid-layout/css/styles.css";
import { PanelView, type Act } from "../panels/PanelView";
import type { Fragment, Panel } from "../panels/types";

const rowHeight = 36;
const rowsFor = { s: 2, m: 7, l: 12 } as const;
const widthFor: Record<Panel["viz"], number> = { stat: 3, gauge: 3, timeseries: 6, bar: 6, table: 12, list: 6, inbox: 8, cards: 12, timeline: 12, text: 6 };

function size(p: Panel) {
  const w = Math.min(12, p.width ?? widthFor[p.viz]);
  const h = rowsFor[p.height ?? (p.viz === "stat" || p.viz === "gauge" ? "s" : p.viz === "inbox" ? "l" : "m")];
  return { w, h };
}

/** layoutOf places saved grids first, then flows the rest in order into the
 * first gap that fits, so a new dashboard reads top to bottom by priority. */
export function layoutOf(panels: Panel[]): Layout {
  const placed: { i: string; x: number; y: number; w: number; h: number }[] = [];
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

/** Dashboard is the app's board. Editing drags and resizes panels; a move is
 * saved as one layout edit when the drag ends. */
export function Dashboard({ fragment, editing, act, onLayout, onRemove }: {
  fragment: Fragment; editing: boolean; act?: Act;
  onLayout: (grids: Record<string, { x: number; y: number; w: number; h: number }>) => void;
  onRemove: (id: string) => void;
}) {
  const { width, containerRef, mounted } = useContainerWidth();
  const results = new Map(fragment.results.map(r => [r.id, r]));
  const layout = layoutOf(fragment.dashboard.panels);
  const save = (next: Layout) => {
    const changed = next.filter(n => { const before = layout.find(l => l.i === n.i); return !before || before.x !== n.x || before.y !== n.y || before.w !== n.w || before.h !== n.h; });
    if (changed.length) onLayout(Object.fromEntries(next.map(n => [n.i, { x: n.x, y: n.y, w: n.w, h: n.h }])));
  };
  return <div ref={containerRef} className={editing ? "dashboard editing" : "dashboard"}>
    {mounted && <GridLayout width={width} layout={layout} gridConfig={{ cols: 12, rowHeight, margin: [12, 12], containerPadding: [0, 0] }}
      dragConfig={{ enabled: editing, handle: ".panel > header" }} resizeConfig={{ enabled: editing }}
      onDragStop={(next: Layout) => save(next)} onResizeStop={(next: Layout) => save(next)}>
      {fragment.dashboard.panels.map(p => <div key={p.id} className="cell">
        <PanelView panel={p} result={results.get(p.id)} act={editing ? undefined : act} fill />
        {editing && <button type="button" className="remove" aria-label={`Remove ${p.title}`} onClick={() => onRemove(p.id)}>×</button>}
      </div>)}
    </GridLayout>}
  </div>;
}
