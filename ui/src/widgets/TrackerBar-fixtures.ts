// Fictional strips for the tracker bar: the worst state in each of the last
// 90 minutes on a machine, and the last 30 turns of a session.
import { now, rng } from "./KpiTile-fixtures";
import type { TrackerCell } from "./TrackerBar";
import { stateWord, type State } from "./types";

const clock = (minutesAgo: number) => new Date(now - minutesAgo * 60_000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });

/** minutes draws 90 cells from a seed, with waits, one stuck run, and the
 * last minutes unseen when the machine went out of reach. */
export function minutes(seed: number, options: { waits?: number; stuck?: boolean; quiet?: boolean; offline?: number } = {}): TrackerCell[] {
  const r = rng(seed);
  const cells: TrackerCell[] = [];
  for (let i = 90; i > 0; i--) {
    let state: TrackerCell["state"] = options.quiet ? (r() < 0.15 ? "idle" : null) : r() < 0.8 ? "working" : "idle";
    if (!options.quiet && options.waits && i % 23 < (options.waits > 1 ? 4 : 2) && i > 10) state = "waiting";
    if (options.stuck && i >= 30 && i < 42) state = "stuck";
    if (options.offline && i <= options.offline) state = "offline";
    cells.push({ state, label: `${clock(i)} · ${state === "offline" ? "unreachable" : state ? stateWord[state] : "no sessions"}` });
  }
  return cells;
}

export const busy = minutes(7, { waits: 2 });
export const troubled = minutes(8, { waits: 1, stuck: true });
export const quiet = minutes(9, { quiet: true });
export const unreachable = minutes(10, { quiet: true, offline: 14 });

/** turns is a session's last 30 turns: most went through, some waited on the person, one failed. */
export const turns: TrackerCell[] = Array.from({ length: 30 }, (_, i) => { const r = rng(i + 100)(); const state: State = r < 0.85 ? "working" : r < 0.95 ? "waiting" : "stuck"; return { state, label: `turn ${i + 1} · ${state === "working" ? "went through" : state === "waiting" ? "waited on you" : "failed"}` }; });
