// ChartCard is a time series with its context: a 36 px header with the
// title, an info tooltip, a mono caption ("25 GB now · peak 30 GB"), the
// 1h/6h/24h range and a ⋯ menu, over a chart annotated with what matters:
// the peak as a dashed line, the periods a session waited as yellow bands,
// and events such as a kill as labelled marks. Empty keeps the axes and
// says why; loading is the chart's own skeleton; an error offers a retry.
import { ActionIcon, Box, Group, Menu, Paper, SegmentedControl, Skeleton, Stack, Text, Title, Tooltip } from "@mantine/core";
import { ChatCircle, DotsThree, Info, Table, Trash } from "@phosphor-icons/react";
import { useCallback, useId } from "react";
import { Chart } from "../../panels/Chart";
import type { Theme } from "../../panels/charts";
import { value } from "../../panels/format";
import { timeSeries, type Event, type Series, type Wait } from "./charts";
import { EmptyState } from "./EmptyState";
import { widgetTheme } from "./palette";

export type Range = "1h" | "6h" | "24h";

export type ChartCardProps = {
  title: string;
  /** What the chart measures, behind the info icon. */
  info?: string;
  /** The one-line summary in mono: "25 GB now · peak 30 GB". */
  caption?: string;
  range: Range;
  onRange: (range: Range) => void;
  series: Series[];
  from: number;
  to: number;
  unit?: "bytes" | "count" | "percent";
  waits?: Wait[];
  events?: Event[];
  /** Draw the peak line; on by default. */
  peak?: boolean;
  state?: "ready" | "loading" | "empty" | "error";
  /** Why the chart is empty, or what went wrong. */
  message?: string;
  onRetry?: () => void;
  onAction?: (kind: "table" | "ask" | "remove") => void;
  height?: number;
};

const ranges: Range[] = ["1h", "6h", "24h"];

export function ChartCard({ title, info, caption, range, onRange, series, from, to, unit, waits, events, peak, state = "ready", message, onRetry, onAction, height = 200 }: ChartCardProps) {
  const id = useId();
  const format = useCallback((n: number) => value(n, unit), [unit]);
  const label = info ? `${title}: ${info}` : title;
  const shown = state === "empty" ? [] : series;
  const option = useCallback((t: Theme) => timeSeries({ series: shown, from, to, unit, format, waits, events, peak, label }, widgetTheme(t)), [shown, from, to, unit, format, waits, events, peak, label]);
  let body;
  if (state === "loading") body = <Box role="img" aria-label="Loading chart"><Skeleton h={height} radius="sm" /></Box>;
  else if (state === "error") body = <Box h={height} style={{ display: "flex", alignItems: "center" }}><EmptyState kind="error" title={message ?? "This chart could not load."} caption="The last reading is not shown; it could be stale." onRetry={onRetry} /></Box>;
  else body = <Box pos="relative" h={height}>
    <Chart option={option} label={label} height={height} />
    {state === "empty" && <Text size="xs" c="dimmed" pos="absolute" top="50%" left="50%" style={{ transform: "translate(-50%, -50%)", whiteSpace: "nowrap" }}>{message ?? `no samples in the last ${range}`}</Text>}
  </Box>;
  return <Paper component="section" withBorder radius="md" p="sm" aria-labelledby={id} style={{ display: "flex", flexDirection: "column", gap: 8 }}>
    <Group justify="space-between" wrap="nowrap" gap="sm" h={28}>
      <Group gap={6} wrap="nowrap" style={{ minWidth: 0 }}>
        <Title order={3} id={id} fz="sm" fw={600} lh={1.2} lineClamp={1} title={title} style={{ letterSpacing: "-0.01em" }}>{title}</Title>
        {info && <Tooltip label={info} multiline maw={320}><ActionIcon size="xs" aria-label={`About ${title}`}><Info size={14} /></ActionIcon></Tooltip>}
        {caption && <Text size="xs" c="dimmed" ff="monospace" truncate title={caption} style={{ minWidth: 0 }} visibleFrom="sm">{caption}</Text>}
      </Group>
      <Group gap={4} wrap="nowrap" style={{ flex: "none" }}>
        <SegmentedControl size="xs" value={range} onChange={v => onRange(v as Range)} data={ranges} aria-label={`${title} range`} styles={{ label: { fontFamily: "var(--mantine-font-family-monospace)", paddingInline: 8 } }} />
        {onAction && <Menu position="bottom-end" withinPortal shadow="md" width={180}>
          <Menu.Target><ActionIcon size="sm" aria-label={`More for ${title}`}><DotsThree size={18} /></ActionIcon></Menu.Target>
          <Menu.Dropdown>
            <Menu.Item leftSection={<Table />} onClick={() => onAction("table")}>Show as table</Menu.Item>
            <Menu.Item leftSection={<ChatCircle />} onClick={() => onAction("ask")}>Ask Chat about it</Menu.Item>
            <Menu.Divider />
            <Menu.Item color="danger" leftSection={<Trash />} onClick={() => onAction("remove")}>Remove</Menu.Item>
          </Menu.Dropdown>
        </Menu>}
      </Group>
    </Group>
    <Stack gap={0}>{body}</Stack>
  </Paper>;
}
