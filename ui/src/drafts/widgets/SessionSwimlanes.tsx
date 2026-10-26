// SessionSwimlanes is a Gantt of sessions: one lane each over the last
// hours, the lane's segments colored by what the session was doing, and a
// line at now. Equal neighbouring segments merge, so a lane reads as dwell
// time per state (Grafana's state timeline). The legend under it carries
// the words for the colors.
import { Group, Stack } from "@mantine/core";
import { useCallback, useMemo } from "react";
import { Chart } from "../../panels/Chart";
import type { Theme } from "../../panels/charts";
import { swimlanes, type Lane } from "./charts";
import { widgetTheme } from "./palette";
import { StatusBadge } from "./StatusBadge";
import type { State } from "./types";

export type { Lane } from "./charts";

export type SessionSwimlanesProps = {
  lanes: Lane[];
  from: number;
  to: number;
  now: number;
  /** What the lanes show, for the chart's description. */
  label: string;
  laneHeight?: number;
};

const legend: State[] = ["working", "waiting", "stuck", "idle"];

/** merged joins neighbouring segments in the same state. */
export function merged(lanes: Lane[]): Lane[] {
  return lanes.map(l => {
    const segments: Lane["segments"] = [];
    for (const s of [...l.segments].sort((a, b) => a.from - b.from)) {
      const last = segments[segments.length - 1];
      if (last && last.state === s.state && last.to >= s.from) last.to = Math.max(last.to, s.to);
      else segments.push({ ...s });
    }
    return { ...l, segments };
  });
}

export function SessionSwimlanes({ lanes, from, to, now, label, laneHeight = 26 }: SessionSwimlanesProps) {
  const tidy = useMemo(() => merged(lanes), [lanes]);
  const option = useCallback((t: Theme) => swimlanes({ lanes: tidy, from, to, now, label }, widgetTheme(t)), [tidy, from, to, now, label]);
  return <Stack gap={6}>
    <Chart option={option} label={label} height={tidy.length * laneHeight + 36} />
    <Group gap="md" aria-hidden>{legend.map(s => <StatusBadge key={s} state={s} />)}</Group>
  </Stack>;
}
