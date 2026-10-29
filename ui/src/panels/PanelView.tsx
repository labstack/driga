import { ActionIcon, Box, Button, Card, Group, Paper, Pill, ScrollArea, SimpleGrid, Stack, Table, Text, Title, Tooltip, UnstyledButton } from "@mantine/core";
import { CaretDown, CaretUp, Info } from "@phosphor-icons/react";
import { createSortedRowModel, rowSortingFeature, sortFn_alphanumeric, sortFn_basic, tableFeatures, useTable, type ColumnDef, type SortingState } from "@tanstack/react-table";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { BarGauge } from "../widgets/BarGauge";
import { BarList } from "../widgets/BarList";
import { ChartCard, type ChartCardProps } from "../widgets/ChartCard";
import type { Series } from "../widgets/charts";
import { EmptyState, splitMessage } from "../widgets/EmptyState";
import { InboxItem, type InboxEntry } from "../widgets/InboxItem";
import { Measure, Sparkline } from "../widgets/Measure";
import { SessionSwimlanes, type Lane } from "../widgets/SessionSwimlanes";
import { StatusBadge, stateOf } from "../widgets/StatusBadge";
import type { State } from "../widgets/types";
import { summary, timeseriesOf } from "./charts";
import { cell, exact, label, value } from "./format";
import { rowActions, useHost, type Act, type ActionKind, type InboxFilter } from "./host";
import { RowMenu, rowProps } from "./RowMenu";
import { rows, type Cell, type Column, type Frame, type Panel, type Prompt, type PromptBurst, type Result, type Row } from "./types";

export type { Act } from "./host";

const heights = { s: 120, m: 240, l: 420 } as const;

export function panelHeight(panel: Panel): number {
  const fallback = panel.viz === "stat" || panel.viz === "gauge" ? "s" : panel.viz === "inbox" ? "l" : "m";
  return heights[panel.height ?? fallback];
}

/** PanelView is one panel as a bordered card: its title, then its body in
 * the state the result is in. In a grid cell it fills the cell; in a chat
 * card it takes its own height. A time series is the chart card itself. */
export function PanelView({ panel, result, act: given, fill }: { panel: Panel; result?: Result; act?: Act; fill?: boolean }) {
  const host = useHost();
  const act = given ?? host.act;
  const height = fill ? 0 : panelHeight(panel);
  const title = result?.title || panel.title;
  if (panel.viz === "timeseries") return <TimeseriesCard panel={panel} result={result} fill={fill} height={height} title={title} />;
  let body: ReactNode;
  if (panel.viz === "text") body = <ScrollArea style={{ flex: 1 }} type="auto" viewportProps={scrollable}><Text component="div" size="sm" className="markdown"><Markdown remarkPlugins={[remarkGfm]} components={{ img: () => null }}>{panel.content ?? ""}</Markdown></Text></ScrollArea>;
  else if (!result) body = <Text size="sm" c="dimmed">Loading…</Text>;
  else if (result.status === "error") body = <EmptyState kind="error" title={result.message ?? "This panel could not run."} />;
  else if (panel.viz === "inbox" && (host.prompts?.length || host.bursts?.length || host.grantHistory?.length || host.inboxFilter)) body = <Inbox frame={result.frame ?? { columns: [], rows: [] }} act={act} prompts={host.prompts} answer={host.answer} filter={host.inboxFilter} />;
  else if (!result.frame || result.status === "empty") body = <Empty panel={panel} message={result.message} />;
  else body = <Body panel={panel} result={result} frame={result.frame} act={act} filter={host.inboxFilter} />;
  const said = panel.viz === "bar" && result?.frame ? summary(result.frame, panel) : "";
  // An article, not a section: a board and a Chat card can both hold a
  // "Sessions" panel, and labelled sections are landmarks that must differ.
  return <Card component="article" aria-labelledby={`panel-${panel.id}`} h={fill ? "100%" : undefined} style={{ display: "flex", flexDirection: "column", minHeight: 0 }}>
    <Group gap={6} wrap="nowrap" mb={6} className="panel-handle">
      <Title order={2} id={`panel-${panel.id}`} fz="md" fw={600} lineClamp={1} style={{ letterSpacing: "-0.01em", flex: "0 1 auto", maxWidth: "75%" }} title={title}>{title}</Title>
      {panel.viz === "inbox" && host.inboxFilter && <Pill withRemoveButton onRemove={host.clearInboxFilter} removeButtonProps={{ "aria-label": "Clear inbox filter" }}>Showing: {{ waiting: "Need you", review: "To review", stuck: "Stuck" }[host.inboxFilter]}</Pill>}
      {panel.description && <Tooltip label={panel.description} multiline maw={320}>
        <ActionIcon size="xs" aria-label={`About ${panel.title}`}><Info size={14} /></ActionIcon>
      </Tooltip>}
      {said && <Text size="xs" c="dimmed" truncate ml="auto" ta="right" style={{ flex: "1 1 0", minWidth: 0 }} title={said}>{said}</Text>}
    </Group>
    <Box style={{ flex: 1, minHeight: fill ? 0 : Math.min(height, 120), display: "flex", flexDirection: "column" }}>{body}</Box>
  </Card>;
}

/** Empty is a panel with nothing to show: the inbox's is the all-clear,
 * the rest say what would appear and how. */
function Empty({ panel, message }: { panel: Panel; message?: string }) {
  const { title, caption } = splitMessage(message ?? (panel.viz === "inbox" ? "Nothing needs you right now." : "Nothing matches right now."));
  return <EmptyState kind={panel.viz === "inbox" ? "all-clear" : "empty"} title={title} caption={caption} />;
}

/** TimeseriesCard is a time series panel as the chart card: the frame's
 * series in the slots, the summary as the caption, and the card's menu
 * showing the data as a table or asking Chat. */
function TimeseriesCard({ panel, result, fill, height, title }: { panel: Panel; result?: Result; fill?: boolean; height: number; title: string }) {
  const host = useHost();
  const [asTable, setAsTable] = useState(false);
  const frame = result?.frame;
  const lines = useMemo(() => frame ? timeseriesOf(frame, panel) : [], [frame, panel]);
  const series: Series[] = useMemo(() => lines.map((l, i) => ({ name: l.name, points: l.points, color: i })), [lines]);
  const stamps = lines.flatMap(l => l.points.map(p => p[0]));
  const unit = frame?.columns[2]?.unit as ChartCardProps["unit"];
  const state = !result ? "loading" : result.status === "error" ? "error" : !lines.length ? "empty" : "ready";
  const message = state === "error" ? result?.message ?? "This panel could not run." : state === "empty" ? result?.message ?? "Nothing matches right now." : undefined;
  return <ChartCard id={panel.id} title={title} info={panel.description} caption={frame && state === "ready" ? summary(frame, panel) : undefined}
    series={series} from={stamps.length ? Math.min(...stamps) : 0} to={stamps.length ? Math.max(...stamps) : 0} unit={unit} state={state} message={message}
    actions={{ table: state === "ready" ? () => setAsTable(v => !v) : undefined, ask: host.ask ? () => host.ask!(`Tell me about "${title}" right now.`) : undefined }}
    height={fill ? "fill" : height - 48} headingOrder={2} headerClassName="panel-handle">
    {asTable && frame ? <DataTable frame={chartTable(frame, panel)} source={panel.source} /> : undefined}
  </ChartCard>;
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

function Body({ panel, result, frame, act, filter }: { panel: Panel; result: Result; frame: Frame; act?: Act; filter?: InboxFilter | null }) {
  switch (panel.viz) {
    case "stat": return <Stat frame={frame} trend={result.trend} title={panel.title} total={result.total} caption={result.caption} />;
    case "gauge": return <Gauge frame={frame} max={panel.max} title={panel.title} />;
    case "bar": return <Bars frame={frame} title={panel.title} />;
    case "inbox": return <Inbox frame={frame} act={act} filter={filter} />;
    case "list": return <List frame={frame} source={panel.source} act={act} />;
    case "cards": return <Cards frame={frame} source={panel.source} act={act} />;
    case "timeline": return <Timeline frame={frame} act={act} title={panel.title} />;
    default: return <DataTable frame={frame} source={panel.source} act={act} />;
  }
}

function Stat({ frame, trend, title, total, caption }: { frame: Frame; trend?: (number | null)[]; title: string; total?: number | null; caption?: number | null }) {
  return <Stack gap={4} style={{ flex: 1 }}>
    <Group gap={6} align="baseline"><Measure n={frame.rows[0]?.[0] ?? null} unit={frame.columns[0]?.unit} total={total} />{caption != null && <Text ff="monospace" size="sm" c="dimmed">· {value(caption)}</Text>}</Group>
    {trend && trend.length > 1 && <Box style={{ flex: 1, minHeight: 24 }}><Sparkline trend={trend} title={title} /></Box>}
  </Stack>;
}

/** Gauge is a reading against its whole: the number, then the bar gauge
 * with its threshold ticks. */
function Gauge({ frame, max, title }: { frame: Frame; max?: number; title: string }) {
  const n = Number(frame.rows[0]?.[0] ?? 0);
  const top = max ?? 100;
  const unit = frame.columns[0]?.unit;
  return <Stack gap={6}>
    <Measure n={n} unit={unit} />
    <BarGauge value={n} max={top} format={v => value(v, unit)} label={title} caption={false} />
    <Text size="xs" c="dimmed">of {value(top, unit)}</Text>
  </Stack>;
}

/** Bars ranks a grouped frame [category, value] as the bar list, largest first. */
function Bars({ frame, title }: { frame: Frame; title: string }) {
  const unit = frame.columns[1]?.unit;
  const items = frame.rows.map((r, i) => ({ id: `${i}`, label: String(r[0] ?? "–") || "–", value: Number(r[1] ?? 0) }));
  return <ScrollArea style={{ flex: 1 }} type="auto" viewportProps={scrollable}><BarList items={items} format={n => value(n, unit)} label={title} /></ScrollArea>;
}

const features = tableFeatures({ rowSortingFeature, sortedRowModel: createSortedRowModel(), sortFns: { basic: sortFn_basic, alphanumeric: sortFn_alphanumeric } });

/** CellView renders a value by its column: states as status badges, times as
 * relative with the exact UTC time on hover, numbers in mono. */
function CellView({ v, column, row }: { v: Cell; column: Column; row?: Row }) {
  if ((column.name === "label" || column.name === "state") && typeof v === "string" && v) {
    const read = stateOf(v);
    const inferred = read.inferred || row?.inferred === true;
    const badge = <StatusBadge state={read.state} label={read.label} inferred={inferred} />;
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
  return <ScrollArea style={{ flex: 1 }} type="auto" aria-label="Table" viewportProps={scrollable}>
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

// A panel's scroll area is reachable by keyboard, so its rows can be read
// with the arrow keys even when none of them is a control.
const scrollable = { tabIndex: 0 } as const;

function rowOf(columns: Column[], r: Frame["rows"][number]): Row {
  return Object.fromEntries(columns.map((c, i) => [c.name, r[i] ?? null]));
}

function List({ frame, source, act }: { frame: Frame; source?: string; act?: Act }) {
  const { local } = useHost();
  const shown = frame.columns.filter(c => !c.hidden);
  const [first, ...rest] = shown;
  return <ScrollArea style={{ flex: 1 }} type="auto" viewportProps={scrollable}><Stack gap={0}>{rows(frame).map((r, k) => {
    const kinds = act ? rowActions(source, r, local) : [];
    return <Group key={k} justify="space-between" wrap="nowrap" py={6} px={4} style={{ borderBottom: "1px solid var(--driga-border-soft)" }} {...rowProps(kinds, r, act)} className={`row-hover ${rowProps(kinds, r, act).className ?? ""}`}>
      <Text size="sm" fw={500} truncate style={{ minWidth: 0 }} title={first ? cell(r[first.name], first) : undefined}>{first ? cell(r[first.name], first) : ""}</Text>
      <Group gap="xs" wrap="nowrap">
        <Text size="xs" c="dimmed" ff="monospace">{rest.map(c => cell(r[c.name], c)).join(" · ")}</Text>
        {kinds.length > 0 && <RowMenu kinds={kinds} row={r} act={act!} label={rowLabel(r)} />}
      </Group>
    </Group>;
  })}</Stack></ScrollArea>;
}

/** entryOf reads an inbox row as the inbox item shows it: a wait is a
 * permission, a question or another wait; a finished turn carries its
 * review facts; anything else is a machine warning. */
export function entryOf(r: Row, now: number): InboxEntry {
  const kind = String(r.kind ?? "");
  const waiting = kind === "waiting";
  const session = Number(r.session_id) > 0 ? `Session ${r.session_id}` : "";
  if (!waiting && kind !== "finished") {
    return { id: String(r.id), kind: "machine", title: `${label(kind)} on ${String(r.machine ?? "a machine")}`, reason: String(r.summary ?? "") || undefined, machine: undefined, at: String(r.occurred_at ?? "") };
  }
  const base = { id: String(r.id), title: String(r.title || r.summary || session), agent: String(r.agent ?? "") || undefined, project: String(r.project ?? "") || undefined, branch: String(r.branch ?? "") || undefined, machine: String(r.machine ?? "") || undefined, at: String(r.occurred_at ?? "") };
  if (waiting) {
    const reason = String(r.reason ?? "");
    return { ...base, kind: reason === "permission" ? "permission" : reason === "question" ? "question" : "waiting", reason: [r.tool, r.detail].filter(Boolean).join(" · ") || undefined, since: typeof r.waited === "number" ? new Date(now - r.waited).toISOString() : undefined };
  }
  const summary = String(r.summary ?? "");
  return {
    ...base, kind: "finished", turns: typeof r.turns === "number" ? r.turns : undefined,
    reason: summary && summary !== "turn finished" ? summary : r.last_commit ? `last commit: ${r.last_commit}` : undefined,
    review: { files: Number(r.files ?? 0), uncommitted: Number(r.dirty ?? 0), unpushed: Number(r.unpushed ?? 0), checkout: String(r.checkout ?? "") || undefined },
  };
}

/** Inbox is the needs-you queue: permission prompts, then waits with why and
 * how long, then machine warnings, then completions. Only what needs the
 * person carries color. The header narrows it to one kind. */
function Inbox({ frame, act, prompts, answer, filter }: { frame: Frame; act?: Act; prompts?: Prompt[]; answer?: (p: Prompt, allow: boolean) => Promise<void>; filter?: InboxFilter | null }) {
  const host = useHost();
  const { stuck } = host;
  const now = Date.now();
  if (filter === "stuck") return stuck?.rows.length ? <DataTable frame={stuck} source="sessions" act={act} /> : <EmptyState kind="empty" title="No session looks stuck." />;
  const groups = (host.bursts ?? []).filter(b => b.members.length > 1);
  const grouped = new Set(groups.flatMap(b => b.members.map(m => m.id)));
  const individual = prompts?.filter(p => !grouped.has(p.id)) ?? [];
  const items = rows(frame);
  const waits = items.filter(r => r.kind === "waiting");
  // Machine warnings, such as memory pressure, need the person too; they are
  // not finished work.
  const warnings = items.filter(r => r.kind !== "waiting" && r.kind !== "finished");
  const done = items.filter(r => r.kind === "finished");
  const jump = (r: Row) => act && Number(r.session_id) > 0 ? () => void act({ kind: "jump", row: r }) : undefined;
  const ack = (r: Row) => act && !r.ack_pending ? () => void act({ kind: "ack", row: r }) : undefined;
  const section = (title: string, state: State, tone: "warning" | "info", n: number, children: ReactNode) => n > 0 && <Stack gap={6}>
    <Box><StatusBadge variant="pill" state={state} tone={tone} label={`${title} · ${n}`} /></Box>{children}
  </Stack>;
  const needs = filter !== "review";
  const review = filter !== "waiting";
  return <ScrollArea style={{ flex: 1 }} type="auto" viewportProps={scrollable}><Stack gap="sm">
    {needs && section("Permission prompts", "waiting", "warning", individual.length + groups.length, <>{groups.map(b => <PromptBurstItem key={b.id} burst={b} answer={answer} decide={host.decideBurst}/>)}{individual.map(p => <PromptItem key={p.id} prompt={p} answer={answer}/>)}</>)}
    {needs && section("Needs you", "waiting", "warning", waits.length, waits.map(r => <InboxItem key={String(r.id)} entry={entryOf(r, now)} onOpen={jump(r)} />))}
    {needs && section("Machines", "waiting", "warning", warnings.length, warnings.map(r => <InboxItem key={String(r.id)} entry={entryOf(r, now)} onDismiss={ack(r)} />))}
    {host.grantHistory?.map(d => <Paper withBorder p="xs" radius="md" key={`${d.grant_id}/${d.action_id ?? d.prompt_id}`}><Text size="xs">{d.attribution} · {d.session ?? d.conversation} · {d.time}</Text><Button size="compact-xs" variant="subtle" disabled={d.revoked} onClick={() => void host.revokeGrant?.(d.grant_id)}>{d.revoked ? "Grant revoked" : "Revoke grant"}</Button></Paper>)}
    {review && section("To review", "finished", "info", done.length, done.map(r => <InboxItem key={String(r.id)} entry={entryOf(r, now)} onApprove={ack(r)} onOpen={jump(r)} />))}
  </Stack></ScrollArea>;
}

/** Cards shows each row as a tile: the first column names it, the rest are
 * its facts. Zero counts stay quiet; nonzero ones read as work in flight. */
function Cards({ frame, source, act }: { frame: Frame; source?: string; act?: Act }) {
  const { local } = useHost();
  const shown = frame.columns.filter(c => !c.hidden);
  const [first, ...rest] = shown;
  return <ScrollArea style={{ flex: 1 }} type="auto" viewportProps={scrollable}>
    <Box style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(220px, 1fr))", gap: 10 }}>{rows(frame).map((r, k) => {
      const kinds = act ? rowActions(source, r, local) : [];
      return <Paper key={k} p="sm" withBorder style={{ background: "var(--driga-inset)" }}>
        <Text size="sm" fw={600} truncate mb={6} title={first ? cell(r[first.name], first) : undefined}>{first ? cell(r[first.name], first) : ""}</Text>
        <SimpleGrid cols={2} spacing={12} verticalSpacing={2}>{rest.map(c => {
          const v = r[c.name];
          if (v === null || v === "" || v === undefined) return null;
          const busy = typeof v === "number" && v > 0 && c.unit === "count";
          return <Group key={c.name} justify="space-between" gap={6} wrap="nowrap">
            <Text size="xs" c="dimmed" truncate title={label(c.name)}>{label(c.name)}</Text>
            <Text size="xs" ff="monospace" c={busy ? "warning" : undefined} truncate title={cell(v, c)}>{cell(v, c)}</Text>
          </Group>;
        })}</SimpleGrid>
        {kinds.length > 0 && <Group justify="flex-end" mt={4}><RowMenu kinds={kinds} row={r} act={act!} label={rowLabel(r)} /></Group>}
      </Paper>;
    })}</Box>
  </ScrollArea>;
}

/** lanesOf reads the flow's sessions as swimlanes: each from its start to
 * its end or now in its state's color, with its waits on the person as
 * waiting stretches cut into it. */
export function lanesOf(frame: Frame, now: number): Lane[] {
  return rows(frame).map(r => {
    const start = Date.parse(String(r.started_at)) || now;
    const end = Date.parse(String(r.ended_at ?? "")) || now;
    const read = stateOf(String(r.label ?? ""));
    const base: State = r.ended_at ? "finished" : read.state && read.state !== "waiting" ? read.state : "working";
    const waits = (frame.spans ?? []).filter(s => s.session_id === r.id && s.machine_id === r.machine_id).map(w => ({ from: Date.parse(String(w.from)) || start, to: Date.parse(String(w.to ?? "")) || now })).sort((a, b) => a.from - b.from);
    const segments: Lane["segments"] = [];
    let cursor = start;
    for (const w of waits) {
      const from = Math.max(cursor,start,w.from), to = Math.min(end,w.to);
      if (to <= from) continue;
      if (from > cursor) segments.push({from:cursor,to:from,state:base});
      segments.push({from,to,state:"waiting"});
      cursor=to;
    }
    if (cursor < end) segments.push({ from: cursor, to: end, state: base });
    return { id: JSON.stringify([r.machine_id,r.id]), name: [r.title || `Session ${r.id}`, r.agent].filter(Boolean).join(" · "), segments };
  });
}

/** Timeline draws the flow's sessions as swimlanes, from the earliest start
 * to now; clicking a lane opens the session's pane. */
function Timeline({ frame, act, title }: { frame: Frame; act?: Act; title: string }) {
  const now = useMemo(() => Date.now(), [frame]);
  const lanes = useMemo(() => lanesOf(frame, now), [frame, now]);
  const starts = lanes.flatMap(l => l.segments.map(s => s.from));
  const from = starts.length ? Math.min(...starts) : now - 3600_000;
  const byId = new Map(rows(frame).map(r => [JSON.stringify([r.machine_id,r.id]), r]));
  return <ScrollArea style={{ flex: 1 }} type="auto" viewportProps={scrollable}>
    <SessionSwimlanes lanes={lanes} from={from} to={now} now={now} label={title} onSelect={act ? id => { const r = byId.get(id); if (r) void act({ kind: "jump", row: { ...r, session_id: r.id } }); } : undefined} />
  </ScrollArea>;
}

/** PromptItem is one pending permission prompt: the tool and exactly what it
 * asks to run. Its answers arm after a moment, so a click meant for the row
 * that was there before never answers the one that moved into place. */
export function PromptItem({ prompt, answer }: { prompt: Prompt; answer?: (p: Prompt, allow: boolean) => Promise<void> }) {
  const host = useHost();
  const [armed, setArmed] = useState(false);
  const [busy, setBusy] = useState(false);
  useEffect(() => { setArmed(false); const t = window.setTimeout(() => setArmed(true), 600); return () => window.clearTimeout(t); }, [prompt.id, JSON.stringify(prompt.input)]);
  const respond = async (allow: boolean) => { if (!answer) return; setBusy(true); try { await answer(prompt, allow); } finally { setBusy(false); } };
  const detail = promptDetail(prompt);
  const entry: InboxEntry = { id: prompt.id, kind: "permission", title: prompt.session, agent: "claude", reason: detail ? `${prompt.tool}(${detail})` : prompt.tool, project: prompt.cwd, at: prompt.created_at, since: prompt.created_at };
  return <Stack gap={4}><InboxItem entry={entry} armed={armed && !busy} onApprove={answer ? () => void respond(true) : undefined} onDeny={answer ? () => void respond(false) : undefined} />
    <Text component="pre" size="xs" ff="monospace" style={{ whiteSpace:"pre-wrap", overflowWrap:"anywhere" }}>{JSON.stringify(prompt.input,null,2)}</Text>
    {prompt.grant_eligible && host.answerSimilar && <Stack gap={4}><Text size="xs" ff="monospace" style={{overflowWrap:"anywhere"}}>{prompt.grant_pattern}</Text><Text size="xs" c="dimmed">Expires {prompt.grant_expires_at}</Text><Button size="compact-xs" variant="light" disabled={!armed || busy} onClick={() => {setBusy(true);void host.answerSimilar!(prompt).finally(()=>setBusy(false));}}>Allow similar for this session</Button></Stack>}
  </Stack>;
}

function promptDetail(p: Prompt): string {
  const input = (p.input ?? {}) as Record<string, unknown>;
  for (const key of ["command", "file_path", "url", "pattern"]) if (typeof input[key] === "string") return String(input[key]);
  return "";
}

/** Bulk decisions capture the visible member IDs. New arrivals re-arm the
 * control and cannot enter an already submitted request. */
export function PromptBurstItem({ burst, answer, decide }: { burst: PromptBurst; answer?: (p: Prompt, allow: boolean) => Promise<void>; decide?: (id: string, members: string[], allow: boolean) => Promise<void> }) {
  const identity = `${burst.id}/${burst.members.map(m => `${m.id}:${m.input_hash}`).join("/")}`;
  const [armed, setArmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  const current = useRef(identity); current.current = identity;
  const submitting = useRef(false);
  useEffect(() => { setArmed(false); setBusy(false); setProblem(undefined); submitting.current = false; const t = window.setTimeout(() => setArmed(true), 600); return () => window.clearTimeout(t); }, [identity]);
  const allow = async () => {
    if (!armed || submitting.current || !decide) return;
    submitting.current = true; setBusy(true);
    const snapshot = burst.members.map(m => m.id), started = identity;
    try { await decide(burst.id, snapshot, true); }
    catch(e) { if(current.current === started) setProblem((e as Error).message); }
    finally { if(current.current === started) { submitting.current = false; setBusy(false); } }
  };
  return <Paper withBorder p="xs" radius="md" style={{background:"var(--driga-raised)"}}><Stack gap="xs">
    <Text size="sm" fw={600}>{burst.members.length} permission prompts · {burst.session}</Text>
    <Text size="xs" c="dimmed" style={{overflowWrap:"anywhere"}}>in {burst.cwd}</Text>
    {burst.members.map(m => <PromptItem key={`${m.id}/${m.input_hash}`} prompt={m.prompt} answer={answer}/>)}
    {decide && <Button disabled={!armed || busy} onClick={() => void allow()}>Allow all {burst.members.length}</Button>}
    {problem && <Text size="xs" c="danger" role="alert">{problem}</Text>}
  </Stack></Paper>;
}
