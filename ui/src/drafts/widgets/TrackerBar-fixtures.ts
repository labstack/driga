// Fictional strips for the tracker bar: the worst state in each of the last
// 90 minutes on a machine, and the last 90 turns of a session.
import { now, rng } from "./KpiTile-fixtures";
import type { TrackerCell } from "./TrackerBar";
import { stateWord, type State } from "./types";

const clock = (minutesAgo: number) => new Date(now - minutesAgo * 60_000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });

/** minutes draws 90 cells from a seed, with waits and one stuck run. */
export function minutes(seed: number, options: { waits?: number; stuck?: boolean; quiet?: boolean } = {}): TrackerCell[] {
  const r = rng(seed);
  const cells: TrackerCell[] = [];
  for (let i = 90; i > 0; i--) {
    let state: State | null = options.quiet ? (r() < 0.15 ? "idle" : null) : r() < 0.8 ? "working" : "idle";
    if (!options.quiet && options.waits && i % 23 < (options.waits > 1 ? 4 : 2) && i > 10) state = "waiting";
    if (options.stuck && i >= 30 && i < 42) state = "stuck";
    cells.push({ state, label: `${clock(i)} · ${state ? stateWord[state] : "no sessions"}` });
  }
  return cells;
}

export const busy = minutes(7, { waits: 2 });
export const troubled = minutes(8, { waits: 1, stuck: true });
export const quiet = minutes(9, { quiet: true });
export const turns: TrackerCell[] = Array.from({ length: 90 }, (_, i) => { const r = rng(i + 100)(); const state: State = r < 0.85 ? "working" : r < 0.95 ? "waiting" : "idle"; return { state, label: `turn ${i + 1} · ${stateWord[state]}` }; });
