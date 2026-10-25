import { ActionIcon, Badge, Box, Button, Card, Group, Paper, Progress, ScrollArea, SimpleGrid, Stack, Table, Text, Title, Tooltip, UnstyledButton } from "@mantine/core";
import { CaretDown, CaretUp, ChartLine, Check, CheckCircle, Info, Table as TableIcon } from "@phosphor-icons/react";
import { createSortedRowModel, rowSortingFeature, sortFn_alphanumeric, sortFn_basic, tableFeatures, useTable, type ColumnDef, type SortingState } from "@tanstack/react-table";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Chart } from "./Chart";
import { bar, sparkline as sparklineOption, summary, timeseries } from "./charts";
import { cell, duration, exact, label, value } from "./format";
import { actionLabel, rowActions, useHost, type Act, type ActionKind } from "./host";
import { RowMenu, rowProps } from "./RowMenu";
import { StatusBadge, toneOf } from "./StatusBadge";
import { rows, type Cell, type Column, type Frame, type Panel, type Prompt, type Result, type Row } from "./types";

export type { Act } from "./host";

const heights = { s: 120, m: 240, l: 420 } as const;

export function panelHeight(panel: Panel): number {
  const fallback = panel.viz === "stat" || panel.viz === "gauge" ? "s" : panel.viz === "inbox" ? "l" : "m";
  return heights[panel.height ?? fallback];
}

/** PanelView is one panel as a bordered card: its title, then its body in
 * the state the result is in. In a grid cell it fills the cell; in a chat
 * card it takes its own height. */
export function PanelView({ panel, result, act: given, fill }: { panel: Panel; result?: Result; act?: Act; fill?: boolean }) {
  const host = useHost();
  const act = given ?? host.act;
  const height = fill ? 0 : panelHeight(panel);
  // A chart can also be read as a table, for exact values and screen readers.
  const [asTable, setAsTable] = useState(false);
  const chart = (panel.viz === "bar" || panel.viz === "timeseries") && result?.status !== "error" && !!result?.frame?.rows.length;
  let body: ReactNode;
  if (panel.viz === "text") body = <Text component="div" size="sm" className="markdown"><Markdown remarkPlugins={[remarkGfm]}>{panel.content ?? ""}</Markdown></Text>;
  else if (!result) body = <Text size="sm" c="dimmed">Loading…</Text>;
  else if (result.status === "error") body = <Text size="sm" c="danger">{result.message ?? "This panel could not run."}</Text>;
  else if (panel.viz === "inbox" && host.prompts?.length) body = <Inbox frame={result.frame ?? { columns: [], rows: [] }} act={act} prompts={host.prompts} answer={host.answer} />;
  else if (!result.frame || result.status === "empty") body = <Text size="sm" c="dimmed">{result.message ?? (panel.viz === "inbox" ? "Nothing needs you right now." : "Nothing matches right now.")}</Text>;
  else if (chart && asTable) body = <DataTable frame={chartTable(result.frame, panel)} source={panel.source} />;
  else body = <Body panel={panel} result={result} frame={result.frame} height={height} act={act} />;
  const said = chart && result?.frame ? summary(result.frame, panel) : "";
  return <Card component="section" aria-labelledby={`panel-${panel.id}`} h={fill ? "100%" : undefined} style={{ display: "flex", flexDirection: "column", minHeight: 0 }}>
    <Group gap={6} wrap="nowrap" mb={6} className="panel-handle">
      <Title order={2} id={`panel-${panel.id}`} fz="md" fw={600} lineClamp={1} style={{ letterSpacing: "-0.01em", flex: "0 1 auto", maxWidth: "75%" }}>{result?.title || panel.title}</Title>
      {panel.description && <Tooltip label={panel.description} multiline maw={320}>
        <ActionIcon size="xs" aria-label={`About ${panel.title}`}><Info size={14} /></ActionIcon>
      </Tooltip>}
      {said && <Text size="xs" c="dimmed" truncate ml="auto" ta="right" style={{ flex: "1 1 0", minWidth: 0 }} title={said}>{said}</Text>}
      {chart && <Tooltip label={asTable ? "Show as chart" : "Show as table"}>
        <ActionIcon size="sm" ml={said ? 0 : "auto"} aria-pressed={asTable} aria-label={asTable ? `Show ${panel.title} as chart` : `Show ${panel.title} as table`} onClick={() => setAsTable(v => !v)}>{asTable ? <ChartLine size={14} /> : <TableIcon size={14} />}</ActionIcon>
      </Tooltip>}
    </Group>
    <Box style={{ flex: 1, minHeight: fill ? 0 : Math.min(height, 120), display: "flex", flexDirection: "column" }}>{body}</Box>
  </Card>;
}

/** chartTable is a chart's frame as a table: newest first for a time series,
 * with the series column only when there are several. */
function chartTable(frame: Frame, panel: Panel): Frame {
  if (panel.viz !== "timeseries") return frame;
  const several = new Set(frame.rows.map(r => r[1])).size > 1;
  const columns = several ? frame.columns : [frame.columns[0], frame.columns[2]];
  const rows = [...frame.rows].reverse().map(r => several ? r : [r[0], r[2]]);
  return { columns: columns.map(c => c.name === "value" ? { ...c, name: panel.measure?.split(":").pop() || "value" } : c.name === "ts" ? { ...c, name: "time" } : c), rows };
}

function Body({ panel, result, frame, height, act }: { panel: Panel; result: Result; frame: Frame; height: number; act?: Act }) {
  const chartHeight = height ? height - 48 : undefined;
  const option = useCallback((t: Parameters<typeof bar>[2]) => panel.viz === "bar" ? bar(frame, panel, t) : timeseries(frame, panel, t), [panel, frame]);
  switch (panel.viz) {
    case "stat": return <Stat frame={frame} trend={result.trend} title={panel.title} total={result.total} />;
    case "gauge": return <Gauge frame={frame} max={panel.max} />;
    case "bar":
    case "timeseries": return <Chart option={option} label={panel.description ? `${panel.title}: ${panel.description}` : panel.title} height={chartHeight} />;
    case "inbox": return <Inbox frame={frame} act={act} />;
    case "list": return <List frame={frame} source={panel.source} act={act} />;
    case "cards": return <Cards frame={frame} source={panel.source} act={act} />;
    case "timeline": return <Timeline frame={frame} act={act} />;
    default: return <DataTable frame={frame} source={panel.source} act={act} />;
  }
}

/** Measure shows a number with its unit at a smaller size, as Canon's stat
 * tile does: "1.8" then "GB". */
export function Measure({ n, unit, size = 28, total }: { n: Cell; unit?: string; size?: number; total?: Cell }) {
  if (typeof n !== "number") return <Text ff="monospace" fz={size} fw={600} lh={1.1} c="dimmed">–</Text>;
  const [number, suffix] = splitUnit(value(n, unit));
  // Out of a total, the value and the total share the total's unit: 25 / 32 GB.
  if (typeof total === "number") {
    const [whole, wholeSuffix] = splitUnit(value(total, unit));
    const [part] = splitUnit(unit === "bytes" ? sameUnit(n, total) : value(n, unit));
    return <Text ff="monospace" fz={size} fw={600} lh={1.1} style={{ fontVariantNumeric: "tabular-nums" }}>
      {part}<Text span ff="monospace" fz="0.6em" fw={500} c="dimmed" mx={3}>/ {whole}</Text>{wholeSuffix && <Text span ff="monospace" fz="0.6em" fw={500} c="dimmed">{wholeSuffix}</Text>}
    </Text>;
  }
  return <Text ff="monospace" fz={size} fw={600} lh={1.1} style={{ fontVariantNumeric: "tabular-nums" }}>
    {number}{suffix && <Text span ff="monospace" fz="0.6em" fw={500} c="dimmed" ml={3}>{suffix}</Text>}
  </Text>;
}

// sameUnit writes n in the unit total is written in, so 25 GB / 32 GB reads
// as 25 / 32 GB.
function sameUnit(n: number, total: number): string {
  const steps = Math.max(0, Math.min(4, Math.floor(Math.log(Math.max(total, 1)) / Math.log(1024))));
  const scaled = n / 1024 ** steps;
  return scaled >= 10 ? String(Math.round(scaled)) : scaled.toFixed(1);
}

function splitUnit(text: string): [string, string] {
  const m = /^([$]?[\d.,]+)\s*(.*)$/.exec(text);
  return m ? [m[1], m[2]] : [text, ""];
}

function Stat({ frame, trend, title, total }: { frame: Frame; trend?: (number | null)[]; title: string; total?: number | null }) {
  return <Stack gap={4} style={{ flex: 1 }}>
    <Measure n={frame.rows[0]?.[0] ?? null} unit={frame.columns[0]?.unit} total={total} />
    {trend && trend.length > 1 && <Box style={{ flex: 1, minHeight: 24 }}><Sparkline trend={trend} title={title} /></Box>}
  </Stack>;
}

export function Sparkline({ trend, title, color }: { trend: (number | null)[]; title: string; color?: string }) {
  const option = useCallback((t: Parameters<typeof bar>[2]) => sparklineOption(trend, title, t, color), [trend, title, color]);
  return <Chart option={option} label={`${title} over the last hour`} small />;
}


function Gauge({ frame, max }: { frame: Frame; max?: number }) {
  const n = Number(frame.rows[0]?.[0] ?? 0);
  const top = max ?? 100;
  const unit = frame.columns[0]?.unit;
  const share = Math.min(100, (n / top) * 100);
  return <Stack gap={6}>
    <Measure n={n} unit={unit} />
    <Progress value={share} color={share >= 80 ? "warning" : "brand"} aria-label={`${value(n, unit)} of ${value(top, unit)}`} />
    <Text size="xs" c="dimmed">of {value(top, unit)}</Text>
  </Stack>;
}

const features = tableFeatures({ rowSortingFeature, sortedRowModel: createSortedRowModel(), sortFns: { basic: sortFn_basic, alphanumeric: sortFn_alphanumeric } });

/** CellView renders a value by its column: states as status badges, times as
 * relative with the exact UTC time on hover, numbers in mono. */
function CellView({ v, column, row }: { v: Cell; column: Column; row?: Row }) {
  if ((column.name === "label" || column.name === "state") && typeof v === "string" && v) {
    const state = v.replace(/^~/, "");
    const inferred = row?.inferred === true || v.startsWith("~");
    const badge = <StatusBadge tone={toneOf(state)} inferred={inferred}>{state}</StatusBadge>;
    const why = typeof row?.evidence === "string" && row.evidence ? row.evidence : inferred ? "Inferred from process and file activity, not reported by the agent." : "";
    return why ? <Tooltip label={why} multiline maw={320}>{badge}</Tooltip> : badge;
  }
  const text = cell(v, column);
  if (column.type === "time" && typeof v === "string") return <Text span fz="inherit" title={exact(v)}>{text}</Text>;
  if (column.type === "number") return <Text span ff="monospace" fz="inherit">{text}</Text>;
  // A long title or branch gives way to the columns beside it; its full text is on hover.
  if (column.name === "title" || column.name === "branch") return <Text fz="inherit" truncate maw={column.name === "title" ? 240 : 140} title={text}>{text}</Text>;
  return <>{text}</>;
}

function DataTable({ frame, source, act }: { frame: Frame; source?: string; act?: Act }) {
  const { local } = useHost();
  const [sorting, setSorting] = useState<SortingState>([]);
  const data = useMemo(() => frame.rows.map(r => rowOf(frame.columns, r)), [frame]);
  const columns = useMemo<ColumnDef<typeof features, Row>[]>(() => frame.columns.filter(c => !c.hidden).map(c => ({
    id: c.name, accessorFn: (r: Row) => r[c.name], header: label(c.name),
    sortFn: c.type === "number" ? "basic" : "alphanumeric", sortDescFirst: c.type === "number",
    cell: ({ row }: { row: { original: Row } }) => <CellView v={row.original[c.name]} column={c} row={row.original} />,
    meta: { column: c },
  })), [frame.columns]);
  const table = useTable({ features, data, columns, state: { sorting }, onSortingChange: setSorting });
  const actionsFor = (r: Row): ActionKind[] => act ? rowActions(source, r, local) : [];
  const anyActions = !!act && data.some(r => actionsFor(r).length > 0);
  return <ScrollArea style={{ flex: 1 }} type="auto" aria-label="Table">
    <Table miw={420}>
      <Table.Thead>{table.getHeaderGroups().map(group => <Table.Tr key={group.id}>
        {group.headers.map(header => {
          const c = (header.column.columnDef.meta as { column: Column }).column;
          const sorted = header.column.getIsSorted();
          return <Table.Th key={header.id} ta={c.type === "number" ? "right" : undefined} aria-sort={sorted === "asc" ? "ascending" : sorted === "desc" ? "descending" : "none"} style={{ whiteSpace: "nowrap" }}>
            <UnstyledButton onClick={header.column.getToggleSortingHandler()} fz="xs" fw={500} c="dimmed">
              <table.FlexRender header={header} />{sorted === "asc" ? <CaretUp size={10} /> : sorted === "desc" ? <CaretDown size={10} /> : null}
            </UnstyledButton>
          </Table.Th>;
        })}
        {anyActions && <Table.Th w={36}><span className="sr-only">Actions</span></Table.Th>}
      </Table.Tr>)}</Table.Thead>
      <Table.Tbody>{table.getRowModel().rows.map(row => <Table.Tr key={row.id} {...rowProps(actionsFor(row.original), row.original, act)}>
        {row.getAllCells().map(c => {
          const column = (c.column.columnDef.meta as { column: Column }).column;
          return <Table.Td key={c.id} ta={column.type === "number" ? "right" : undefined} style={{ whiteSpace: "nowrap", maxWidth: 320, overflow: "hidden", textOverflow: "ellipsis" }}><table.FlexRender cell={c} /></Table.Td>;
        })}
        {anyActions && <Table.Td ta="right" py={0}><RowMenu kinds={actionsFor(row.original)} row={row.original} act={act!} label={rowLabel(row.original)} /></Table.Td>}
      </Table.Tr>)}</Table.Tbody>
    </Table>
  </ScrollArea>;
}

// rowLabel names a row for its menu's accessible name.
function rowLabel(r: Row): string {
  return String(r.title || r.signature || r.project || r.path || r.id || "this row");
}

function rowOf(columns: Column[], r: Frame["rows"][number]): Row {
  return Object.fromEntries(columns.map((c, i) => [c.name, r[i] ?? null]));
}

function List({ frame, source, act }: { frame: Frame; source?: string; act?: Act }) {
  const { local } = useHost();
  const shown = frame.columns.filter(c => !c.hidden);
  const [first, ...rest] = shown;
  return <ScrollArea style={{ flex: 1 }} type="auto"><Stack gap={0}>{rows(frame).map((r, k) => {
    const kinds = act ? rowActions(source, r, local) : [];
    return <Group key={k} justify="space-between" wrap="nowrap" py={6} px={4} style={{ borderBottom: "1px solid var(--driga-border-soft)" }} {...rowProps(kinds, r, act)} className={`row-hover ${rowProps(kinds, r, act).className ?? ""}`}>
      <Text size="sm" fw={500} truncate>{first ? cell(r[first.name], first) : ""}</Text>
      <Group gap="xs" wrap="nowrap">
        <Text size="xs" c="dimmed" ff="monospace">{rest.map(c => cell(r[c.name], c)).join(" · ")}</Text>
        {kinds.length > 0 && <RowMenu kinds={kinds} row={r} act={act!} label={rowLabel(r)} />}
      </Group>
    </Group>;
  })}</Stack></ScrollArea>;
}

/** Inbox is the needs-you queue: permission prompts, then waits with why and
 * how long, then completions. Only what needs the person carries color. */
function Inbox({ frame, act, prompts, answer }: { frame: Frame; act?: Act; prompts?: Prompt[]; answer?: (p: Prompt, allow: boolean) => Promise<void> }) {
  const items = rows(frame);
  const waits = items.filter(r => r.kind === "waiting");
  // Machine warnings, such as memory pressure, need the person too; they are
  // not finished work.
  const warnings = items.filter(r => r.kind !== "waiting" && r.kind !== "finished");
  const done = items.filter(r => r.kind === "finished");
  const warning = (r: Row) => {
    const live = r.needs_you === true;
    return <Paper key={String(r.id)} p="xs" radius="sm" withBorder style={{ background: "var(--driga-inset)", borderLeft: live ? "3px solid var(--mantine-color-warning-filled)" : undefined }}>
      <Group justify="space-between" wrap="nowrap" align="flex-start" gap="sm">
        <Stack gap={2} style={{ minWidth: 0 }}>
          <Group gap={6} wrap="nowrap">
            <StatusBadge tone={live ? "warning" : "neutral"}>{`${label(String(r.kind))}${live ? "" : ", ended"}`}</StatusBadge>
            {r.machine && <Text size="xs" c="dimmed">{String(r.machine)}</Text>}
            {typeof r.age === "number" && <Text size="xs" c="dimmed">{duration(r.age)} ago</Text>}
          </Group>
          {r.summary && <Text size="xs" style={{ overflowWrap: "anywhere" }}>{String(r.summary)}</Text>}
        </Stack>
        {act && !r.ack_pending && <Button size="compact-xs" variant="default" onClick={() => void act({ kind: "ack", row: r })}>Dismiss</Button>}
      </Group>
    </Paper>;
  };
  const item = (r: Row) => {
    const waiting = r.kind === "waiting";
    const why = waiting ? [r.reason || "waiting", r.tool, r.detail].filter(Boolean).join(" · ") : String(r.summary || r.kind || "");
    const kinds: ActionKind[] = act ? [...(Number(r.session_id) > 0 ? ["jump" as const] : []), ...(!waiting && !r.ack_pending ? ["ack" as const] : [])] : [];
    const name = String(r.title || r.summary || "Session " + r.session_id);
    const where = [r.project, r.agent, r.machine].filter(Boolean).join(" · ");
    const props = rowProps(kinds, r, act);
    // A finished turn is one line with what there is to review; a wait keeps
    // a second line for what it waits on.
    if (!waiting) return <Group key={String(r.id)} gap={8} wrap="nowrap" px={6} py={4} {...props} className={`row-hover ${props.className ?? ""}`} style={{ borderRadius: "var(--mantine-radius-sm)" }}>
      <CheckCircle size={14} aria-hidden style={{ color: "var(--driga-muted)", flex: "none" }} />
      <Text size="sm" fw={500} truncate style={{ flex: "0 1 auto" }}>{name}</Text>
      <Text size="xs" c="dimmed" truncate style={{ flex: 1 }}>{[where, r.branch, typeof r.turns === "number" && r.turns > 1 ? `${r.turns} turns` : "", r.summary && r.summary !== "turn finished" ? r.summary : r.last_commit ? `last commit: ${r.last_commit}` : ""].filter(Boolean).join(" · ")}</Text>
      <Review row={r} />
      {typeof r.age === "number" && <Text size="xs" c="dimmed" style={{ whiteSpace: "nowrap" }}>{duration(r.age)} ago</Text>}
      {act && !r.ack_pending && <Button size="compact-xs" variant="subtle" color="gray" leftSection={<Check size={12} />} aria-label={`Mark ${name} reviewed`} onClick={e => { e.stopPropagation(); void act({ kind: "ack", row: r }); }}>Reviewed</Button>}
      {act && <RowMenu kinds={kinds} row={r} act={act} label={name} />}
    </Group>;
    return <Paper key={String(r.id)} p="xs" radius="sm" withBorder {...props} className={`row-hover ${props.className ?? ""}`} style={{ background: "var(--driga-inset)", borderLeft: "3px solid var(--mantine-color-warning-filled)" }}>
      <Group justify="space-between" wrap="nowrap" align="flex-start" gap="sm">
        <Stack gap={2} style={{ minWidth: 0 }}>
          <Group gap={6} wrap="nowrap">
            <StatusBadge tone="warning">{`waiting ${typeof r.waited === "number" ? duration(r.waited) : ""}`.trim()}</StatusBadge>
            <Text size="sm" fw={500} truncate>{name}</Text>
          </Group>
          <Text size="xs" ff={r.tool || r.detail ? "monospace" : undefined} c="dimmed" style={{ overflowWrap: "anywhere" }}>{why}</Text>
          <Text size="xs" c="dimmed">{where}</Text>
        </Stack>
        {act && <RowMenu kinds={kinds} row={r} act={act} label={name} />}
      </Group>
    </Paper>;
  };
  const section = (title: string, n: number, children: ReactNode) => n > 0 && <Stack gap={6}>
    <Text size="xs" fw={600} c="dimmed">{title} · {n}</Text>{children}
  </Stack>;
  return <ScrollArea style={{ flex: 1 }} type="auto"><Stack gap="sm">
    {section("Permission prompts", prompts?.length ?? 0, prompts?.map(p => <PromptItem key={p.id} prompt={p} answer={answer} />))}
    {section("Needs you", waits.length, waits.map(item))}
    {section("Machines", warnings.length, warnings.map(warning))}
    {section("To review", done.length, done.map(item))}
  </Stack></ScrollArea>;
}

/** Review states what a finished turn left behind: the files its session
 * edited, and work in its checkout that is not committed or not pushed. Only
 * the latter two carry color, since they are what a person must act on. */
function Review({ row }: { row: Row }) {
  const files = typeof row.files === "number" ? row.files : 0;
  const dirty = typeof row.dirty === "number" ? row.dirty : 0;
  const unpushed = typeof row.unpushed === "number" ? row.unpushed : 0;
  const checkout = typeof row.checkout === "string" ? row.checkout : "";
  // With nothing edited and nothing left over, say which: a clean checkout,
  // or no checkout Driga knows to look at.
  if (!files && !dirty && !unpushed) return checkout
    ? <Tooltip label={<Text size="xs" ff="monospace">{checkout}</Text>} withinPortal><Badge size="xs" variant="light" color="gray">clean</Badge></Tooltip>
    : <Text size="xs" c="dimmed" style={{ whiteSpace: "nowrap" }}>no checkout</Text>;
  const edited = typeof row.edited === "string" && row.edited ? row.edited.split("\n") : [];
  const tip = <Stack gap={2} maw={420}>
    {checkout && <Text size="xs" ff="monospace">{checkout}</Text>}
    {typeof row.last_commit === "string" && row.last_commit && <Text size="xs">Last commit: {row.last_commit}</Text>}
    {edited.slice(0, 8).map(f => <Text key={f} size="xs" ff="monospace" truncate>{checkout && f.startsWith(checkout + "/") ? f.slice(checkout.length + 1) : f}</Text>)}
    {edited.length > 8 && <Text size="xs" c="dimmed">and {edited.length - 8} more</Text>}
  </Stack>;
  return <Tooltip label={tip} multiline withinPortal>
    <Group gap={6} wrap="nowrap" style={{ flex: "none" }} aria-label={[files && plural(files, "file"), dirty && `${dirty} uncommitted`, unpushed && `${unpushed} unpushed`].filter(Boolean).join(", ")}>
      {files > 0 && <Text size="xs" c="dimmed" style={{ whiteSpace: "nowrap" }}>{plural(files, "file")}</Text>}
      {dirty > 0 && <Badge size="xs" variant="light" color="warning">{dirty} uncommitted</Badge>}
      {unpushed > 0 && <Badge size="xs" variant="light" color="gray">{unpushed} unpushed</Badge>}
    </Group>
  </Tooltip>;
}

function plural(n: number, word: string): string { return `${n} ${word}${n === 1 ? "" : "s"}`; }

/** Cards shows each row as a tile: the first column names it, the rest are
 * its facts. Zero counts stay quiet; nonzero ones read as work in flight. */
function Cards({ frame, source, act }: { frame: Frame; source?: string; act?: Act }) {
  const { local } = useHost();
  const shown = frame.columns.filter(c => !c.hidden);
  const [first, ...rest] = shown;
  return <ScrollArea style={{ flex: 1 }} type="auto">
    <Box style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(220px, 1fr))", gap: 10 }}>{rows(frame).map((r, k) => {
      const kinds = act ? rowActions(source, r, local) : [];
      return <Paper key={k} p="sm" withBorder style={{ background: "var(--driga-inset)" }}>
        <Text size="sm" fw={600} truncate mb={6}>{first ? cell(r[first.name], first) : ""}</Text>
        <SimpleGrid cols={2} spacing={12} verticalSpacing={2}>{rest.map(c => {
          const v = r[c.name];
          if (v === null || v === "" || v === undefined) return null;
          const busy = typeof v === "number" && v > 0 && c.unit === "count";
          return <Group key={c.name} justify="space-between" gap={6} wrap="nowrap">
            <Text size="xs" c="dimmed" truncate>{label(c.name)}</Text>
            <Text size="xs" ff="monospace" c={busy ? "warning" : undefined} truncate>{cell(v, c)}</Text>
          </Group>;
        })}</SimpleGrid>
        {kinds.length > 0 && <Group justify="flex-end" mt={4}><RowMenu kinds={kinds} row={r} act={act!} label={rowLabel(r)} /></Group>}
      </Paper>;
    })}</Box>
  </ScrollArea>;
}

/** Timeline draws each session as a bar from its start to its end (or now),
 * with its waits on you marked over it. Working is grey; only waits carry
 * color. It is Driga's own composition: no library chart draws spans with
 * marks on a shared time axis like this. */
function Timeline({ frame, act }: { frame: Frame; act?: Act }) {
  const items = rows(frame);
  const now = Date.now();
  const starts = items.map(r => Date.parse(String(r.started_at))).filter(n => !Number.isNaN(n));
  const from = starts.length ? Math.min(...starts) : now - 3600_000;
  const span = Math.max(60_000, now - from);
  const pos = (t: number) => `${Math.max(0, Math.min(100, ((t - from) / span) * 100))}%`;
  const end = (r: Row) => Date.parse(String(r.ended_at ?? "")) || now;
  const ticks = [0, 0.25, 0.5, 0.75, 1].map(f => from + f * span);
  return <ScrollArea style={{ flex: 1 }} type="auto"><div className="timeline">
    <div className="tl-axis" aria-hidden="true">{ticks.map(t => <span key={t} style={{ left: pos(t) }}>{new Date(t).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</span>)}</div>
    <ul>{items.map(r => {
      const start = Date.parse(String(r.started_at));
      const finish = end(r);
      const waits = (frame.spans ?? []).filter(s => s.session_id === r.id);
      const waiting = r.label === "waiting";
      return <li key={String(r.id)} className="tl-row">
        <UnstyledButton className="tl-label" disabled={!act} onClick={() => act && void act({ kind: "jump", row: { ...r, session_id: r.id } })} title={String(r.title ?? "")}>
          <Text size="xs" fw={500} truncate>{String(r.title || `Session ${r.id}`)}</Text>
          <Text size="xs" c="dimmed" truncate>{[r.agent, r.project].filter(Boolean).join(" · ")}</Text>
        </UnstyledButton>
        <div className="tl-track" role="img" aria-label={`${r.title}: ${r.label}, ${waits.length} waits`}>
          <span className={`tl-bar ${waiting ? "waiting" : r.ended_at ? "ended" : ""}`} style={{ left: pos(start), width: `calc(${pos(finish)} - ${pos(start)})` }} />
          {waits.map((w, i) => { const a = Date.parse(String(w.from)); const b = Date.parse(String(w.to ?? "")) || now; return <span key={i} className="tl-wait" title={w.reason} style={{ left: pos(a), width: `max(3px, calc(${pos(b)} - ${pos(a)}))` }} />; })}
        </div>
        <StatusBadge tone={toneOf(String(r.label ?? ""))}>{String(r.label ?? "")}</StatusBadge>
      </li>;
    })}</ul>
  </div></ScrollArea>;
}

/** PromptItem is one pending permission prompt: the tool and exactly what it
 * asks to run. Its buttons arm after a moment, so a click meant for the row
 * that was there before never answers the one that moved into place. */
function PromptItem({ prompt, answer }: { prompt: Prompt; answer?: (p: Prompt, allow: boolean) => Promise<void> }) {
  const [armed, setArmed] = useState(false);
  const [busy, setBusy] = useState(false);
  useEffect(() => { const t = window.setTimeout(() => setArmed(true), 600); return () => window.clearTimeout(t); }, [prompt.id]);
  const detail = promptDetail(prompt);
  const respond = async (allow: boolean) => { if (!answer) return; setBusy(true); try { await answer(prompt, allow); } finally { setBusy(false); } };
  return <Paper p="xs" radius="sm" withBorder style={{ background: "var(--driga-inset)", borderLeft: "3px solid var(--mantine-color-warning-filled)" }}>
    <Group justify="space-between" wrap="nowrap" align="flex-start" gap="sm">
      <Stack gap={2} style={{ minWidth: 0 }}>
        <Group gap={6}><Badge color="warning">{prompt.tool}</Badge><Text size="sm" fw={500} truncate>{prompt.session}</Text></Group>
        {detail && <Text size="xs" ff="monospace" style={{ overflowWrap: "anywhere" }}>{detail}</Text>}
        <Text size="xs" c="dimmed" truncate>{prompt.cwd}</Text>
      </Stack>
      {answer && <Group gap={4} wrap="nowrap">
        <Button disabled={!armed} loading={busy} onClick={() => void respond(true)}>Allow</Button>
        <Button variant="default" disabled={!armed || busy} onClick={() => void respond(false)}>Deny</Button>
      </Group>}
    </Group>
  </Paper>;
}

function promptDetail(p: Prompt): string {
  const input = (p.input ?? {}) as Record<string, unknown>;
  for (const key of ["command", "file_path", "url", "pattern"]) if (typeof input[key] === "string") return String(input[key]);
  return "";
}
