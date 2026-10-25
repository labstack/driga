import { useMemo, useState } from "react";
import { Chart } from "./Chart";
import { cell, duration, label, value } from "./format";
import { rows, type Column, type Frame, type Panel, type Result, type Row } from "./types";

/** An action a panel row offers. The host decides which it can run: the app
 * runs them all through its approval step; a chat card may offer none. */
export type Action = { kind: "jump" | "ack" | "open"; row: Row };

export type Act = (action: Action) => Promise<void>;

const heights = { s: 120, m: 240, l: 420 } as const;

export function panelHeight(panel: Panel): number {
  const fallback = panel.viz === "stat" || panel.viz === "gauge" ? "s" : panel.viz === "inbox" ? "l" : "m";
  return heights[panel.height ?? fallback];
}

export function PanelView({ panel, result, act, fill }: { panel: Panel; result?: Result; act?: Act; fill?: boolean }) {
  // In a grid cell the panel fills its cell; in a card it takes its own height.
  const height = fill ? 0 : panelHeight(panel);
  let body;
  if (panel.viz === "text") body = <Text content={panel.content ?? ""} />;
  else if (!result) body = <p className="muted">Loading…</p>;
  else if (result.status === "error") body = <p className="problem">{result.message ?? "This panel could not run."}</p>;
  else if (!result.frame || result.status === "empty") body = <p className="muted empty">{panel.viz === "inbox" ? "Nothing needs you right now." : result.message ?? "Nothing matches right now."}</p>;
  else body = <Body panel={panel} frame={result.frame} height={height} act={act} />;
  return <section className={`panel viz-${panel.viz}${fill ? " fill" : ""}`} aria-labelledby={`panel-${panel.id}`}>
    <header><h2 id={`panel-${panel.id}`}>{panel.title}</h2>{panel.description && <span className="muted" title={panel.description}>?</span>}</header>
    <div className="panel-body" style={fill || panel.viz === "stat" ? undefined : { minHeight: Math.min(height, 120) }}>{body}</div>
  </section>;
}

function Body({ panel, frame, height, act }: { panel: Panel; frame: Frame; height: number; act?: Act }) {
  switch (panel.viz) {
    case "stat": return <Stat frame={frame} />;
    case "gauge": return <Gauge frame={frame} max={panel.max} />;
    case "bar":
    case "timeseries": return <Chart panel={panel} frame={frame} height={height ? height - 48 : undefined} />;
    case "inbox": return <Inbox frame={frame} act={act} />;
    case "list": return <List frame={frame} />;
    case "cards": return <Cards frame={frame} />;
    case "timeline": return <Timeline frame={frame} act={act} />;
    default: return <Table frame={frame} act={act} />;
  }
}

function Stat({ frame }: { frame: Frame }) {
  const n = frame.rows[0]?.[0];
  return <p className="stat">{typeof n === "number" ? value(n, frame.columns[0]?.unit) : "–"}</p>;
}

function Gauge({ frame, max }: { frame: Frame; max?: number }) {
  const n = Number(frame.rows[0]?.[0] ?? 0);
  const top = max ?? 100;
  const unit = frame.columns[0]?.unit;
  return <div className="gauge">
    <meter min={0} max={top} value={n} high={top * 0.8} optimum={0}>{value(n, unit)}</meter>
    <span>{value(n, unit)} of {value(top, unit)}</span>
  </div>;
}

function Table({ frame, act }: { frame: Frame; act?: Act }) {
  const [sort, setSort] = useState<{ col: number; desc: boolean } | null>(null);
  const sorted = useMemo(() => {
    if (!sort) return frame.rows;
    return [...frame.rows].sort((a, b) => {
      const x = a[sort.col], y = b[sort.col];
      const c = typeof x === "number" && typeof y === "number" ? x - y : String(x ?? "").localeCompare(String(y ?? ""));
      return sort.desc ? -c : c;
    });
  }, [frame, sort]);
  const id = frame.columns.findIndex(c => c.name === "id");
  return <div className="table-wrap"><table>
    <thead><tr>{frame.columns.map((c, i) => <th key={c.name} scope="col" className={c.type === "number" ? "num" : undefined} aria-sort={sort?.col === i ? (sort.desc ? "descending" : "ascending") : "none"}>
      <button type="button" onClick={() => setSort(s => ({ col: i, desc: s?.col === i ? !s.desc : c.type === "number" }))}>{label(c.name)}</button>
    </th>)}</tr></thead>
    <tbody>{sorted.map((r, i) => <tr key={id >= 0 ? String(r[id]) : i} tabIndex={act ? 0 : undefined} onKeyDown={e => { if (act && e.key === "Enter") void act({ kind: "open", row: rowOf(frame.columns, r) }); }}>
      {frame.columns.map((c, j) => <td key={c.name} className={c.type === "number" ? "num" : undefined}>{cell(r[j], c)}</td>)}
    </tr>)}</tbody>
  </table></div>;
}

function rowOf(columns: Column[], r: Frame["rows"][number]): Row {
  return Object.fromEntries(columns.map((c, i) => [c.name, r[i] ?? null]));
}

function List({ frame }: { frame: Frame }) {
  const [first, ...rest] = frame.columns;
  return <ul className="list">{frame.rows.map((r, i) => <li key={i}>
    <span className="primary">{cell(r[0], first)}</span>
    <span className="muted">{rest.map((c, j) => cell(r[j + 1], c)).join(" · ")}</span>
  </li>)}</ul>;
}

/** Inbox is the needs-you queue: waits first, each with why it waits and how
 * long, then completions. Grey is normal; only waits carry color. */
function Inbox({ frame, act }: { frame: Frame; act?: Act }) {
  const items = rows(frame);
  const waits = items.filter(r => r.kind === "waiting");
  const done = items.filter(r => r.kind !== "waiting");
  const item = (r: Row) => {
    const waiting = r.kind === "waiting";
    const why = waiting ? [r.reason || "waiting", r.tool, r.detail].filter(Boolean).join(" · ") : String(r.summary || r.kind || "");
    return <li key={String(r.id)} className={waiting ? "inbox-item waiting" : "inbox-item"}>
      <div className="inbox-text">
        <span className="primary">{String(r.title || r.summary || "Session " + r.session_id)}</span>
        <span className="why">{why}</span>
        <span className="muted">{[r.project, r.agent, r.machine].filter(Boolean).join(" · ")}{waiting && typeof r.waited === "number" ? ` · waiting ${duration(r.waited)}` : ""}</span>
      </div>
      {act && <div className="actions">
        {Number(r.session_id) > 0 && <button type="button" onClick={() => void act({ kind: "jump", row: r })}>Open pane</button>}
        {!waiting && !r.ack_pending && <button type="button" onClick={() => void act({ kind: "ack", row: r })}>Ack</button>}
      </div>}
    </li>;
  };
  return <div className="inbox">
    {waits.length > 0 && <><h3>Needs you · {waits.length}</h3><ul>{waits.map(item)}</ul></>}
    {done.length > 0 && <><h3>Finished · {done.length}</h3><ul>{done.map(item)}</ul></>}
  </div>;
}

/** Text renders the small Markdown subset text panels need: paragraphs,
 * bullets and bold, as text nodes only. */
function Text({ content }: { content: string }) {
  const blocks = content.split(/\n{2,}/);
  const inline = (s: string) => s.split(/(\*\*[^*]+\*\*)/).map((part, i) => part.startsWith("**") && part.endsWith("**") ? <strong key={i}>{part.slice(2, -2)}</strong> : part);
  return <div className="text">{blocks.map((b, i) => {
    const lines = b.split("\n");
    if (lines.every(l => /^\s*[-*] /.test(l))) return <ul key={i}>{lines.map((l, j) => <li key={j}>{inline(l.replace(/^\s*[-*] /, ""))}</li>)}</ul>;
    return <p key={i}>{inline(b)}</p>;
  })}</div>;
}

/** Cards shows each row as a tile: the first column names it, the rest are
 * its facts. Zero counts stay quiet; nonzero ones read as work in flight. */
function Cards({ frame }: { frame: Frame }) {
  const [first, ...rest] = frame.columns;
  return <ul className="cards">{frame.rows.map((r, i) => <li key={i} className="card">
    <span className="primary">{cell(r[0], first)}</span>
    <dl>{rest.map((c, j) => {
      const v = r[j + 1];
      if (v === null || v === "" || v === undefined) return null;
      return <div key={c.name} className={typeof v === "number" && v > 0 && c.unit === "count" ? "fact busy" : "fact"}><dt>{label(c.name)}</dt><dd>{cell(v, c)}</dd></div>;
    })}</dl>
  </li>)}</ul>;
}

/** Timeline draws each session as a bar from its start to its end (or now),
 * with its waits on you marked over it. Working is grey; only waits carry
 * color. */
function Timeline({ frame, act }: { frame: Frame; act?: Act }) {
  const items = rows(frame);
  const now = Date.now();
  const starts = items.map(r => Date.parse(String(r.started_at))).filter(n => !Number.isNaN(n));
  const from = starts.length ? Math.min(...starts) : now - 3600_000;
  const span = Math.max(60_000, now - from);
  const pos = (t: number) => `${Math.max(0, Math.min(100, ((t - from) / span) * 100))}%`;
  const end = (r: Row) => Date.parse(String(r.ended_at ?? "")) || now;
  const ticks = [0, 0.25, 0.5, 0.75, 1].map(f => from + f * span);
  return <div className="timeline">
    <div className="tl-axis" aria-hidden="true"><span />{ticks.map(t => <span key={t} style={{ left: pos(t) }}>{new Date(t).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</span>)}</div>
    <ul>{items.map(r => {
      const start = Date.parse(String(r.started_at));
      const finish = end(r);
      const waits = (frame.spans ?? []).filter(s => s.session_id === r.id);
      const waiting = r.label === "waiting";
      return <li key={String(r.id)} className="tl-row">
        <button type="button" className="tl-label" disabled={!act} onClick={() => act && void act({ kind: "jump", row: { ...r, session_id: r.id } })} title={String(r.title ?? "")}>
          <span className="primary">{String(r.title || `Session ${r.id}`)}</span>
          <span className="muted">{[r.agent, r.project].filter(Boolean).join(" · ")}</span>
        </button>
        <div className="tl-track" role="img" aria-label={`${r.title}: ${r.label}, ${waits.length} waits`}>
          <span className={`tl-bar ${waiting ? "waiting" : r.ended_at ? "ended" : ""}`} style={{ left: pos(start), width: `calc(${pos(finish)} - ${pos(start)})` }} />
          {waits.map((w, i) => { const a = Date.parse(String(w.from)); const b = Date.parse(String(w.to ?? "")) || now; return <span key={i} className="tl-wait" title={w.reason} style={{ left: pos(a), width: `max(3px, calc(${pos(b)} - ${pos(a)}))` }} />; })}
        </div>
        <span className={`tl-state ${waiting ? "waiting" : ""}`}>{String(r.label ?? "")}</span>
      </li>;
    })}</ul>
  </div>;
}
