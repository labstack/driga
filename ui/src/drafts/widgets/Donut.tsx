// Donut is a share of a whole with the whole in its centre: memory across
// the four attribution buckets (sessions, MCP servers, agents without a
// session, leftovers). The ring carries no labels; the list beside it does,
// with each value and share in mono, so the chart is readable without
// hovering and without color.
import { Box, ColorSwatch, Group, Stack, Text, useComputedColorScheme } from "@mantine/core";
import { useCallback } from "react";
import { Chart } from "../../panels/Chart";
import type { Theme } from "../../panels/charts";
import { donut } from "./charts";
import { chartSeries, widgetTheme } from "./palette";

export type Slice = { name: string; value: number };

export type DonutProps = {
  slices: Slice[];
  format: (n: number) => string;
  /** What is split, for the chart's description: "Fleet memory by bucket". */
  label: string;
  /** The centre text; the formatted total and "total" by default. */
  centre?: { value: string; caption: string };
  size?: number;
};

export function Donut({ slices, format, label, centre, size = 150 }: DonutProps) {
  const scheme = useComputedColorScheme("dark");
  const total = slices.reduce((s, x) => s + x.value, 0);
  const middle = centre ?? { value: format(total), caption: "total" };
  const description = `${label}: ${middle.value} ${middle.caption}`;
  const option = useCallback((t: Theme) => donut(slices, format, description, widgetTheme(t)), [slices, format, description]);
  return <Group gap="md" wrap="nowrap" align="center" w="100%" style={{ minWidth: 0 }}>
    <Box pos="relative" w={size} h={size} style={{ flex: "none" }}>
      <Chart option={option} label={description} height={size} />
      <Stack gap={0} align="center" justify="center" pos="absolute" inset={0} aria-hidden style={{ pointerEvents: "none" }}>
        <Text ff="monospace" fz="lg" fw={600} lh={1.1} style={{ fontVariantNumeric: "tabular-nums" }}>{middle.value}</Text>
        <Text size="xs" c="dimmed" lh={1.2}>{middle.caption}</Text>
      </Stack>
    </Box>
    <Stack gap={6} role="list" aria-label={`${label}, by bucket`} style={{ flex: 1, minWidth: 0 }}>
      {slices.map((s, i) => <Group key={s.name} role="listitem" gap={8} wrap="nowrap" justify="space-between">
        <Group gap={6} wrap="nowrap" style={{ minWidth: 0 }}>
          <ColorSwatch size={8} color={chartSeries[scheme][i] ?? "var(--driga-faint)"} withShadow={false} aria-hidden style={{ flex: "none" }} />
          <Text size="xs" truncate title={s.name} style={{ minWidth: 0 }}>{s.name}</Text>
        </Group>
        <Text size="xs" ff="monospace" c="dimmed" style={{ whiteSpace: "nowrap", flex: "none" }}>{format(s.value)} · {total > 0 ? Math.round((s.value / total) * 100) : 0}%</Text>
      </Group>)}
    </Stack>
  </Group>;
}
