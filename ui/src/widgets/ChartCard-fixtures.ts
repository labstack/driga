// A fictional day of memory on studio for the chart card: one reading a
// minute, a peak early in the afternoon, two periods a session waited and
// a kill that freed memory, sliced to each range.
import type { Event, Point, Series, Wait } from "./charts";
import type { ChartCardProps, Range } from "./ChartCard";
import { GB, now, walk } from "./KpiTile-fixtures";

const minutes = 24 * 60;
const start = now - minutes * 60_000;

const used = walk(11, minutes + 1, 24 * GB, 0.25 * GB, 19 * GB, 29 * GB).map((v, i) => {
  // A build at 13:00 pushes memory to its peak; a kill at 13:05 frees it.
  const t = i / 60;
  if (t >= 21.6 && t < 22.1) return Math.min(30 * GB, v + 5 * GB);
  if (t >= 22.1 && t < 22.5) return v - 1.5 * GB;
  return v;
});

const sessions = used.map((v, i) => Math.max(2 * GB, v * 0.42 + Math.sin(i / 37) * 0.6 * GB));

const pointsOf = (values: number[]): Point[] => values.map((v, i) => [start + i * 60_000, Math.round(v)]);

export const machineMemory: Series = { name: "Used", points: pointsOf(used), color: "brand" };
export const sessionsMemory: Series = { name: "Sessions", points: pointsOf(sessions), color: 2 };

export const waits: Wait[] = [
  { from: now - 11 * 3600_000 - 20 * 60_000, to: now - 11 * 3600_000 - 8 * 60_000, label: "codex waited" },
  { from: now - 48 * 60_000, to: now - 41 * 60_000, label: "claude waited" },
];

export const events: Event[] = [{ at: now - 115 * 60_000, label: "killed 3 procs" }];

const hours: Record<Range, number> = { "1h": 1, "6h": 6, "24h": 24 };

/** chart is the card's data for a range, cut from the day. */
export function chart(range: Range, several = false): Omit<ChartCardProps, "onRange"> {
  const from = now - hours[range] * 3600_000;
  const cut = (s: Series): Series => ({ ...s, points: s.points.filter(p => p[0] >= from) });
  const series = several ? [cut(machineMemory), cut(sessionsMemory)] : [cut(machineMemory)];
  const last = series[0].points.at(-1)?.[1] ?? 0;
  const peak = Math.max(...series[0].points.map(p => p[1]));
  return {
    title: "Memory on studio", info: "Used memory on this machine, one reading a minute. Shaded bands are periods a session waited on you.",
    caption: `${(last / GB).toFixed(0)} GB now · peak ${(peak / GB).toFixed(0)} GB`,
    range, series, from, to: now, unit: "bytes",
    waits: waits.filter(w => w.to >= from), events: events.filter(e => e.at >= from),
  };
}
