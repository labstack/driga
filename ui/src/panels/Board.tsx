import { relative } from "./format";
import { PanelView, type Act } from "./PanelView";
import type { Fragment, Panel } from "./types";

const defaultWidth: Record<Panel["viz"], number> = { stat: 3, gauge: 3, timeseries: 6, bar: 6, table: 12, list: 6, inbox: 8, text: 6 };

/** Board lays a fragment's panels on the 12-column grid. Positions saved by
 * dragging win; otherwise panels flow in order at their widths. */
export function Board({ fragment, onVars, act, compact }: { fragment: Fragment; onVars?: (vars: Record<string, string>) => void; act?: Act; compact?: boolean }) {
  const { dashboard } = fragment;
  const results = new Map(fragment.results.map(r => [r.id, r]));
  const vars = fragment.vars ?? {};
  return <div className={compact ? "board compact" : "board"}>
    <div className="board-head">
      <div>
        <h1>{dashboard.name}</h1>
        {dashboard.description && !compact && <p className="muted">{dashboard.description}</p>}
      </div>
      <div className="board-meta">
        {onVars && dashboard.variables?.map(v => <label key={v.name}>
          <span>{v.name}</span>
          <select value={vars[v.name] ?? ""} onChange={e => onVars({ ...vars, [v.name]: e.target.value })}>
            <option value="">All</option>
            {(fragment.options?.[v.name] ?? []).map(o => <option key={o} value={o}>{o}</option>)}
          </select>
        </label>)}
        <span className="muted" title={fragment.observed_at}>observed {relative(fragment.observed_at)}</span>
      </div>
    </div>
    <div className="grid">
      {dashboard.panels.map(p => {
        const w = Math.min(12, p.grid?.w ?? p.width ?? defaultWidth[p.viz]);
        return <div key={p.id} className="cell" style={{ gridColumn: p.grid ? `${p.grid.x + 1} / span ${w}` : `span ${w}`, gridRow: p.grid ? `${p.grid.y + 1} / span ${p.grid.h}` : undefined }}>
          <PanelView panel={p} result={results.get(p.id)} act={act} />
        </div>;
      })}
    </div>
  </div>;
}
