// Heatmap is projects by hour: one cell per project and hour of the last
// day, its depth the tool calls in that hour, on a single-hue ramp of the
// accent. One hue, not a rainbow, so the eye reads depth as amount. A short
// ramp under the chart says which way is busier.
import { Box, Group, Stack, Text } from "@mantine/core";
import { useCallback } from "react";
import { Chart } from "../../panels/Chart";
import type { Theme } from "../../panels/charts";
import { heatmap } from "./charts";
import { widgetTheme } from "./palette";

export type HeatmapProps = {
  rows: string[];
  cols: string[];
  /** values[row][col] */
  values: number[][];
  format: (n: number) => string;
  /** What a cell counts: "tool calls". */
  unit: string;
  /** What the grid shows, for the chart's description. */
  label: string;
  rowHeight?: number;
};

const ramp = [8, 30, 55, 80, 100];

export function Heatmap({ rows, cols, values, format, unit, label, rowHeight = 22 }: HeatmapProps) {
  const option = useCallback((t: Theme) => heatmap({ rows, cols, values, format, unit, label }, widgetTheme(t)), [rows, cols, values, format, unit, label]);
  return <Stack gap={4}>
    <Chart option={option} label={label} height={rows.length * rowHeight + 32} />
    <Group justify="flex-end" gap={6} aria-hidden>
      <Text size="xs" c="dimmed" lh={1}>fewer</Text>
      {ramp.map(p => <Box key={p} w={12} h={10} style={{ borderRadius: 2, background: `color-mix(in srgb, var(--mantine-color-brand-filled) ${p}%, var(--driga-raised))` }} />)}
      <Text size="xs" c="dimmed" lh={1}>more {unit}</Text>
    </Group>
  </Stack>;
}
