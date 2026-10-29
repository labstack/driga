// A fictional fleet for the header, observed at the fixtures' clock, which
// the preview pins Date.now to.
import { fragments, storyNow } from "../fixtures/fragments";
import type { LiveState } from "../widgets/types";
import { headerOf, type Attention, type AttentionKind, type HeaderData, type Machine, type Poll } from "./Header";
import type { Fragment } from "../panels/types";
import { historyOf } from "./History";

const GB = 1024 ** 3;
const checkedAt = new Date(storyNow - 4000).toISOString();
const ago = (seconds: number) => new Date(storyNow - seconds * 1000).toISOString();

/** walk is a deterministic random walk of n readings ending at `end`, so
 * every run draws the same sparkline. */
function walk(seed: number, n: number, end: number, step: number, min: number, max: number): number[] {
  let s = seed;
  const out: number[] = [];
  let v = end;
  for (let i = 0; i < n; i++) {
    s = (s * 1103515245 + 12345) % 2147483648;
    v = Math.min(max, Math.max(min, v + (s / 2147483648 - 0.5) * 2 * step));
    out.push(v);
  }
  out.reverse();
  out[n - 1] = end;
  return out;
}

/** steps is an hour of a count that holds each value for some minutes, as
 * counts do: [value, minutes] pairs adding up to 60. */
function steps(...segments: [number, number][]): number[] {
  return segments.flatMap(([value, minutes]) => Array<number>(minutes).fill(value));
}

/** count is an attention count with its hour; its delta is the change
 * since the hour began. */
function count(trend: number[]): Attention {
  return { count: trend[trend.length - 1], trend, delta: trend[trend.length - 1] - trend[0] };
}

function attention(waiting: number[], review: number[], stuck: number[]): Record<AttentionKind, Attention> {
  return { waiting: count(waiting), review: count(review), stuck: count(stuck) };
}

const quiet: Record<AttentionKind, Attention> = {
  waiting: { count: 0, trend: Array(60).fill(0), delta: 0 },
  review: { count: 0, trend: Array(60).fill(0), delta: 0 },
  stuck: { count: 0, trend: Array(60).fill(0), delta: 0 },
};

/** polls reads a 30-character strip: w working, W waiting, s stuck, i idle, q quiet, o offline. */
function polls(strip: string): Poll[] {
  const of: Record<string, Poll> = { w: "working", W: "waiting", s: "stuck", i: "idle", q: "quiet", o: "offline" };
  return [...strip].map(c => of[c]);
}

const none: Record<LiveState, number> = { working: 0, waiting: 0, stuck: 0, idle: 0 };

function machine(name: string, m: Partial<Machine> & { strip: string; used: number; total: number; seenAgo: number }): Machine {
  const { strip, used, total, seenAgo, ...rest } = m;
  return { id: `${name}-id`, name, reachable: true, sessions: { ...none }, memory: { used: used * GB, total: total * GB }, polls: polls(strip), seenAt: ago(seenAgo), ...rest };
}

export const studio = machine("studio", { local: true, sessions: { working: 5, waiting: 1, stuck: 0, idle: 2 }, used: 24, total: 32, strip: "wwwwwwwwwwwwwwwwwwwwwwwwwwWWWW", seenAgo: 4 });
export const build01 = machine("build-01", { sessions: { working: 2, waiting: 0, stuck: 1, idle: 0 }, used: 41, total: 64, strip: "wwwwwwwwwwwwwwwwwwwwwwwwwsssss", seenAgo: 9 });
export const laptop02 = machine("laptop-02", { sessions: { working: 0, waiting: 0, stuck: 0, idle: 1 }, used: 9, total: 16, strip: "wwwwwwwwwwiiiiiiiiiiiiiiiiiiii", seenAgo: 20 });
export const build02 = machine("build-02", { reachable: false, used: 12, total: 64, strip: "wwwwwwwwwwwwwwwwwwoooooooooooo", seenAgo: 710 });

const memory = { used: 24 * GB, total: 32 * GB, trend: walk(5, 60, 24 * GB, 0.9 * GB, 20 * GB, 29 * GB) };

/** Each preview starts with a header fragment, just as the app does. */
function fragment(attention: Record<AttentionKind, Attention>, used = memory.used, leftover = 2, reclaim = 6.1 * GB): Fragment {
  const f = structuredClone(fragments.header);
  f.observed_at = checkedAt;
  f.results = f.dashboard.panels.map(p => {
    const kind = p.id === "finished" ? "review" : p.id as AttentionKind;
    const value = attention[kind]?.count ?? (p.id === "leftover" ? 1.8 * GB : p.id === "reclaim" ? reclaim : used);
    return { id: p.id, status: "ok", frame: { columns: [{ name: "value", type: "number", unit: attention[kind] ? "count" : "bytes" }], rows: [[value]] },
      ...(p.id === "memory" ? { total: memory.total, trend: memory.trend } : {}), ...(p.id === "leftover" ? { caption: leftover } : {}) };
  });
  f.history = { attention: attention.waiting.trend.map((_, i) => ({ ts: ago(3600 - i * 60), waiting: attention.waiting.trend[i] ?? 0, review: attention.review.trend[i] ?? 0, stuck: attention.stuck.trend[i] })), polls: [] };
  return f;
}
function data(f: Fragment, machines: Machine[]): HeaderData {
  return { ...headerOf(f, null, historyOf(f)), localMachine: "studio", machines };
}

export const busyFragment = fragment(attention(steps([1, 18], [2, 12], [1, 10], [2, 8], [3, 12]), steps([6, 15], [7, 10], [8, 10], [9, 10], [10, 15]), steps([1, 40], [0, 20])));
export const busy = data(busyFragment, [studio, build01, laptop02, build02]);
export const allClearFragment = fragment(quiet, memory.used, 0, 0);
export const allClear = data(allClearFragment, [{ ...studio, sessions: { ...none, working: 3 } }, { ...build01, sessions: { ...none, working: 1 } }, laptop02]);
export const oneStuckFragment = fragment({ ...quiet, stuck: count(steps([0, 45], [1, 15])) }, memory.used, 0, 0);
export const oneStuck = data(oneStuckFragment, [studio, build01, laptop02]);
export const hotFragment = fragment(attention(steps([3, 60]), steps([10, 60]), steps([0, 60])), 30 * GB);
export const hot = data(hotFragment, busy.machines);
export const freshFragment = fragment(attention([3], [3], [1]));
freshFragment.results = freshFragment.results.map(r => ({ ...r, trend: [] }));
export const fresh = data(freshFragment, []);
const lab = (n: number) => machine(`lab-0${n}`, { sessions: { ...none, working: 3 }, used: 10 + n * 3, total: 64, strip: "wwwwwwwwwwwwwwwwwwwwwwwwwwwwww", seenAgo: 10 + n });
export const manyMachines: HeaderData = { ...busy, machines: [...busy.machines, ...Array.from({ length: 6 }, (_, i) => lab(i + 1))] };

/** The Go catalog run over the same fictional snapshot; no handwritten specs. */
export const catalogFragment = fragments.header_catalog;
export const headerCatalog = catalogFragment.dashboard.panels;
export const allWidgets = data(catalogFragment, busy.machines);
export function widgetData(id: string): HeaderData {
  return data({ ...catalogFragment, dashboard: { ...catalogFragment.dashboard, panels: headerCatalog.filter(p => p.id === id) } }, busy.machines);
}
