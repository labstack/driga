// ChartCard is a chart with its context: a header with the title, an info
// tooltip, a mono caption ("25 GB now · peak 30 GB"), an optional 1h/6h/24h
// range and a ⋯ menu, over a chart annotated with what matters: the peak
// as a dashed line, the periods a session waited as yellow bands, and
// events such as a kill as labelled marks. Empty keeps the axes and says
// why; loading is the chart's own skeleton; an error offers a retry. It is
// the card every chart panel is: a time series from its points, or any
// other chart from an option of the caller's.
import { ActionIcon, Box, Card, Group, Menu, SegmentedControl, Skeleton, Text, Title, Tooltip } from "@mantine/core";
import { ChartLine, ChatCircle, DotsThree, Info, Table, Trash } from "@phosphor-icons/react";
import { useCallback, useId, type ReactNode } from "react";
import { Chart } from "../panels/Chart";
import type { Option, Theme } from "../panels/charts";
import { value } from "../panels/format";
import { timeSeries, type Event, type Series, type Wait } from "./charts";
import { EmptyState } from "./EmptyState";

export type Range = "1h" | "6h" | "24h";

export type ChartCardProps = {
  /** The panel's id, which names the heading as panel-<id> for the app's scrolling. */
  id?: string;
  title: string;
  /** What the chart measures, behind the info icon. */
  info?: string;
  /** The one-line summary in mono: "25 GB now · peak 30 GB". */
  caption?: string;
  /** The range control; none when the data covers one fixed window. */
  range?: Range;
  onRange?: (range: Range) => void;
  series?: Series[];
  from?: number;
  to?: number;
  unit?: "bytes" | "count" | "percent" | "duration";
  waits?: Wait[];
  events?: Event[];
  /** Draw the peak line; on by default. */
  peak?: boolean;
  /** A chart of the caller's own, drawn instead of the series. */
  option?: (t: Theme) => Option;
  state?: "ready" | "loading" | "empty" | "error";
  /** Why the chart is empty, or what went wrong. */
  message?: string;
  onRetry?: () => void;
  /** The ⋯ menu's actions, present when given. */
  actions?: { table?: () => void; ask?: () => void; remove?: () => void };
  /** Shown instead of the chart: its data as a table. */
  children?: ReactNode;
  /** A fixed height, or fill to take the card's space in a grid cell. */
  height?: number | "fill";
  headingOrder?: 2 | 3;
  /** A class for the header row, which a board drags the card by. */
  headerClassName?: string;
};

const ranges: Range[] = ["1h", "6h", "24h"];

export function ChartCard({ id, title, info, caption, range, onRange, series = [], from = 0, to = 0, unit, waits, events, peak, option: given, state = "ready", message, onRetry, actions, children, height = 200, headingOrder = 3, headerClassName }: ChartCardProps) {
  const own = useId();
  const headingId = id ? `panel-${id}` : own;
  const format = useCallback((n: number) => value(n, unit), [unit]);
  const label = info ? `${title}: ${info}` : title;
  const shown = state === "empty" ? [] : series;
  const option = useCallback((t: Theme) => given ? given(t) : timeSeries({ series: shown, from, to, unit, format, waits, events, peak, label }, t), [given, shown, from, to, unit, format, waits, events, peak, label]);
  const fill = height === "fill";
  const px = fill ? undefined : height;
  let body: ReactNode;
  if (children) body = children;
  else if (state === "loading") body = <Box role="img" aria-label="Loading chart" style={fill ? { flex: 1 } : undefined}><Skeleton h={px ?? "100%"} radius="sm" /></Box>;
  else if (state === "error") body = <Box h={px} style={{ display: "flex", alignItems: "center", flex: fill ? 1 : undefined }}><EmptyState kind="error" title={message ?? "This chart could not load."} caption="The last reading is not shown; it could be stale." onRetry={onRetry} /></Box>;
  else body = <Box pos="relative" h={px} style={fill ? { flex: 1, minHeight: 0 } : undefined}>
    <Chart option={option} label={label} height={fill ? "100%" : height} />
    {state === "empty" && <Text size="xs" c="dimmed" pos="absolute" top="50%" left="50%" style={{ transform: "translate(-50%, -50%)", whiteSpace: "nowrap" }}>{message ?? (range ? `no samples in the last ${range}` : "no samples")}</Text>}
  </Box>;
  const menu = actions && (actions.table || actions.ask || actions.remove);
  // An article, not a section: a board and a Chat card can both hold a
  // "Memory" chart, and labelled sections are landmarks that must differ.
  return <Card component="article" aria-labelledby={headingId} h={fill ? "100%" : undefined} style={{ display: "flex", flexDirection: "column", minHeight: 0, gap: 6 }}>
    <Group justify="space-between" wrap="nowrap" gap="sm" className={headerClassName} style={{ flex: "none" }}>
      <Group gap={6} wrap="nowrap" style={{ minWidth: 0 }}>
        <Title order={headingOrder} id={headingId} fz="md" fw={600} lh={1.2} lineClamp={1} title={title} style={{ letterSpacing: "-0.01em", flex: "0 1 auto" }}>{title}</Title>
        {info && <Tooltip label={info} multiline maw={320}><ActionIcon size="xs" aria-label={`About ${title}`}><Info size={14} /></ActionIcon></Tooltip>}
        {caption && <Text size="xs" c="dimmed" ff="monospace" truncate title={caption} style={{ minWidth: 0 }} visibleFrom="sm">{caption}</Text>}
      </Group>
      <Group gap={4} wrap="nowrap" style={{ flex: "none" }}>
        {range && onRange && <SegmentedControl size="xs" value={range} onChange={v => onRange(v as Range)} data={ranges} aria-label={`${title} range`} styles={{ label: { fontFamily: "var(--mantine-font-family-monospace)", paddingInline: 8 } }} />}
        {menu && <Menu position="bottom-end" withinPortal shadow="md" width={180}>
          <Menu.Target><ActionIcon size="sm" aria-label={`More for ${title}`}><DotsThree size={18} /></ActionIcon></Menu.Target>
          <Menu.Dropdown>
            {actions.table && <Menu.Item leftSection={children ? <ChartLine /> : <Table />} onClick={actions.table}>{children ? "Show as chart" : "Show as table"}</Menu.Item>}
            {actions.ask && <Menu.Item leftSection={<ChatCircle />} onClick={actions.ask}>Ask Chat about it</Menu.Item>}
            {actions.remove && <><Menu.Divider /><Menu.Item color="danger" leftSection={<Trash />} onClick={actions.remove}>Remove</Menu.Item></>}
          </Menu.Dropdown>
        </Menu>}
      </Group>
    </Group>
    <Box style={{ flex: 1, minHeight: 0, display: "flex", flexDirection: "column" }}>{body}</Box>
  </Card>;
}
