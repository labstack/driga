// Mirrors internal/panel: the spec Driga's MCP tools accept and the fragment
// they return. A fragment is a dashboard with its results.

export type Viz = "stat" | "gauge" | "timeseries" | "bar" | "table" | "list" | "inbox" | "cards" | "timeline" | "text";

export type Filter = { column: string; op?: string; value: string };

export type Grid = { x: number; y: number; w: number; h: number };

export type Panel = {
  id: string;
  title: string;
  description?: string;
  viz: Viz;
  source?: string;
  where?: Filter[];
  by?: string;
  measure?: string;
  columns?: string[];
  sort?: string;
  limit?: number;
  unit?: string;
  max?: number;
  total?: string;
  empty?: string;
  width?: number;
  height?: "s" | "m" | "l";
  content?: string;
  grid?: Grid;
};

export type Variable = { name: string; field: string; default?: string };

export type Dashboard = { name: string; description?: string; variables?: Variable[]; panels: Panel[] };

export type Column = { name: string; type: "string" | "number" | "time" | "bool"; unit?: string; hidden?: boolean };

export type Cell = string | number | boolean | null;

export type Span = { session_id: number; kind: string; from: string | null; to: string | null; reason?: string };

export type Frame = { columns: Column[]; rows: Cell[][]; spans?: Span[] };

export type Result = { id: string; status: "ok" | "empty" | "error"; message?: string; frame?: Frame; trend?: (number | null)[]; total?: number | null; title?: string };

export type Fragment = {
  history: { attention: { ts: string; waiting: number; review: number; stuck: number | null }[] | null; polls: { machine_id: string; ts: string; state: "working" | "waiting" | "stuck" | "idle" | "quiet" | "offline" }[] | null };
  dashboard: Dashboard;
  results: Result[];
  vars?: Record<string, string>;
  options?: Record<string, string[]>;
  observed_at: string;
  id?: string;
  version?: number;
  local_machine?: string;
};

/** Prompt is a pending Claude permission prompt, answered from the inbox. */
export type Prompt = { id: string; session: string; session_id?: number; cwd: string; tool: string; input?: unknown; created_at: string };

/** isFragment checks the shape a tool result must have before it renders. */
export function isFragment(value: unknown): value is Fragment {
  if (!value || typeof value !== "object") return false;
  const f = value as Partial<Fragment>;
  return !!f.dashboard && Array.isArray(f.dashboard.panels) && Array.isArray(f.results) && typeof f.observed_at === "string";
}

/** normalize gives every list a value, so a server that sends null for an
 * empty list never breaks a render. */
export function normalize(f: Fragment): Fragment {
  return {
    ...f,
    dashboard: { ...f.dashboard, panels: f.dashboard.panels ?? [], variables: f.dashboard.variables ?? [] },
    results: (f.results ?? []).map(r => r.frame ? { ...r, frame: { ...r.frame, columns: r.frame.columns ?? [], rows: r.frame.rows ?? [] } } : r),
  };
}

/** Row is one frame row keyed by column name. */
export type Row = Record<string, Cell>;

export function rows(frame: Frame | undefined): Row[] {
  if (!frame) return [];
  return frame.rows.map(r => Object.fromEntries(frame.columns.map((c, i) => [c.name, r[i] ?? null])));
}
