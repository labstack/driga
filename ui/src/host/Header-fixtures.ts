// A fictional fleet for the header, observed at the fixtures' clock, which
// the preview pins Date.now to.
import { storyNow } from "../fixtures/fragments";
import type { LiveState } from "../widgets/types";
import type { Attention, AttentionKind, HeaderData, Hygiene, LocalMemory, Machine, Poll } from "./Header";

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

const memory: LocalMemory = { machine: "studio", used: 24 * GB, total: 32 * GB, trend: walk(5, 60, 24 * GB, 0.9 * GB, 20 * GB, 29 * GB), delta: -1.8 * GB };

const hygiene: Hygiene = { leftover: { count: 2, bytes: 1.8 * GB }, reclaimable: 6.1 * GB };

/** busy: three agents wait on the person, ten turns await review, nothing
 * is stuck, two leftover process trees, one machine unreachable. */
export const busy: HeaderData = {
  attention: attention(steps([1, 18], [2, 12], [1, 10], [2, 8], [3, 12]), steps([6, 15], [7, 10], [8, 10], [9, 10], [10, 15]), steps([1, 40], [0, 20])),
  hygiene,
  memory,
  machines: [studio, build01, laptop02, build02],
  checkedAt,
};

/** allClear: nothing needs the person and nothing to clean up. */
export const allClear: HeaderData = {
  attention: quiet,
  hygiene: { leftover: { count: 0, bytes: 0 }, reclaimable: 0 },
  memory: { ...memory, delta: 0.2 * GB },
  machines: [
    { ...studio, sessions: { working: 3, waiting: 0, stuck: 0, idle: 1 }, polls: polls("wwwwwwwwwwwwwwwwwwwwwwwwwwwwww") },
    { ...build01, sessions: { working: 1, waiting: 0, stuck: 0, idle: 0 }, polls: polls("wwwwwwwwwwwwwwwwwwwwwwwwwwwwww") },
    laptop02,
  ],
  checkedAt,
};

/** oneStuck: one session has gone quiet for too long; everything else is zero. */
export const oneStuck: HeaderData = {
  attention: { ...quiet, stuck: count(steps([0, 45], [1, 15])) },
  hygiene: { leftover: { count: 0, bytes: 0 }, reclaimable: 0 },
  memory: { ...memory, delta: 2.6 * GB },
  machines: [
    { ...studio, sessions: { working: 4, waiting: 0, stuck: 0, idle: 1 }, polls: polls("wwwwwwwwwwwwwwwwwwwwwwwwwwwwww") },
    build01,
    laptop02,
  ],
  checkedAt,
};

/** hot: the local machine past the danger threshold, so the memory bar turns red. */
export const hot: HeaderData = { ...busy, memory: { ...memory, used: 30 * GB, delta: 3.1 * GB } };

/** fresh: the first load, before the fleet or any kept reading: no delta,
 * no dots, the leftover count unknown. */
export const fresh: HeaderData = {
  attention: { waiting: { count: 3, trend: [3], delta: 0 }, review: { count: 3, trend: [3], delta: 0 }, stuck: { count: 1, trend: [1], delta: 0 } },
  hygiene: { leftover: { bytes: 2 * GB }, reclaimable: 5 * GB },
  memory: { ...memory, delta: 0 },
  machines: [],
  checkedAt,
};

const lab = (n: number, strip: string, sessions: Partial<Record<LiveState, number>>) =>
  machine(`lab-0${n}`, { sessions: { ...none, ...sessions }, used: 10 + n * 3, total: 64, strip, seenAgo: 10 + n });

/** manyMachines: ten machines, more than the fleet pulse shows as dots. */
export const manyMachines: HeaderData = {
  ...busy,
  machines: [
    studio, build01, laptop02, build02,
    lab(1, "wwwwwwwwwwwwwwwwwwwwwwwwwwwwww", { working: 3 }),
    lab(2, "wwwwwwwwwwwwwwwwwwwwwwwwwwwwWW", { working: 1, waiting: 1 }),
    lab(3, "iiiiiiiiiiiiiiiiiiiiiiiiiiiiii", { idle: 2 }),
    lab(4, "qqqqqqqqqqqqqqqqqqqqqqqqqqqqqq", {}),
    lab(5, "wwwwwwwwwwwwwwwwwwwwwwwwwwwwww", { working: 2 }),
    lab(6, "wwwwwwwwwwwwwwwwwwwwwwwwwwwwww", { working: 4, idle: 1 }),
  ],
};
