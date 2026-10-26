// Three fictional hours of sessions for the swimlanes.
import { now } from "./KpiTile-fixtures";
import type { Lane } from "./SessionSwimlanes";
import type { State } from "./types";

const m = 60_000;
const at = (minutesAgo: number) => now - minutesAgo * m;

/** lane builds one session's segments from (start, state) pairs in minutes ago, newest last. */
function lane(id: string, name: string, steps: [number, State][], ends?: number): Lane {
  const segments = steps.map(([from, state], i) => ({ from: at(from), to: at(steps[i + 1]?.[0] ?? ends ?? 0), state }));
  return { id, name, segments };
}

export const lanes: Lane[] = [
  lane("s2", "Add retries to the harbor client · claude", [[48, "working"], [31, "waiting"], [27, "working"], [20, "working"], [2.2, "waiting"]]),
  lane("s3", "Rewrite the dashboard layout tests · agy", [[170, "working"], [96, "waiting"], [88, "working"], [40, "idle"], [14, "stuck"]]),
  lane("s1", "Fix the race in the rate limiter · codex", [[122, "working"], [75, "waiting"], [71, "working"]]),
  lane("s5", "Migrate the index store to WAL · copilot", [[140, "working"], [110, "waiting"], [104, "working"]], 58),
  lane("s4", "Draft the release notes · gemini", [[70, "working"], [35, "idle"]]),
  lane("s6", "Profile the collector on build-01 · claude", [[160, "working"], [151, "waiting"], [149, "working"]], 123),
];

export const swimlanes = { lanes, from: at(180), to: now, now, label: "Sessions over the last three hours" };
