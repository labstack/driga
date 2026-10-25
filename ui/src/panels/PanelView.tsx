import { ActionIcon, Badge, Box, Button, Card, Group, Paper, Progress, ScrollArea, SimpleGrid, Stack, Table, Text, Title, Tooltip, UnstyledButton } from "@mantine/core";
import { CaretDown, CaretUp, Info } from "@phosphor-icons/react";
import { createSortedRowModel, rowSortingFeature, sortFn_alphanumeric, sortFn_basic, tableFeatures, useTable, type ColumnDef, type SortingState } from "@tanstack/react-table";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import Markdown from "react-markdown";
import { Chart } from "./Chart";
import { bar, sparkline as sparklineOption, timeseries } from "./charts";
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
  let body: ReactNode;
  if (panel.viz === "text") body = <Text component="div" size="sm" className="markdown"><Markdown>{panel.content ?? ""}</Markdown></Text>;
  else if (!result) body = <Text size="sm" c="dimmed">Loading…</Text>;
  else if (result.status === "error") body = <Text size="sm" c="danger">{result.message ?? "This panel could not run."}</Text>;
  else if (panel.viz === "inbox" && host.prompts?.length) body = <Inbox frame={result.frame ?? { columns: [], rows: [] }} act={act} prompts={host.prompts} answer={host.answer} />;
  else if (!result.frame || result.status === "empty") body = <Text size="sm" c="dimmed">{panel.viz === "inbox" ? "Nothing needs you right now." : result.message ?? "Nothing matches right now."}</Text>;
  else body = <Body panel={panel} result={result} frame={result.frame} height={height} act={act} />;
  return <Card component="section" aria-labelledby={`panel-${panel.id}`} h={fill ? "100%" : undefined} style={{ display: "flex", flexDirection: "column", minHeight: 0 }}>
    <Group gap={6} wrap="nowrap" mb={6} className="panel-handle">
      <Title order={2} id={`panel-${panel.id}`} fz="sm" fw={600} lineClamp={1}>{panel.title}</Title>
      {panel.description && <Tooltip label={panel.description} multiline maw={320}>
        <ActionIcon size="xs" aria-label={`About ${panel.title}`}><Info size={14} /></ActionIcon>
      </Tooltip>}
    </Group>
    <Box style={{ flex: 1, minHeight: fill ? 0 : Math.min(height, 120), display: "flex", flexDirection: "column" }}>{body}</Box>
  </Card>;
}

function Body({ panel, result, frame, height, act }: { panel: Panel; result: Result; frame: Frame; height: number; act?: Act }) {
  const chartHeight = height ? height - 48 : undefined;
  const option = useCallback((t: Parameters<typeof bar>[2]) => panel.viz === "bar" ? bar(frame, panel, t) : timeseries(frame, panel, t), [panel, frame]);
  switch (panel.viz) {
    case "stat": return <Stat frame={frame} trend={result.trend} title={panel.title} />;
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
export function Measure({ n, unit, size = 28 }: { n: Cell; unit?: string; size?: number }) {
  if (typeof n !== "number") return <Text ff="monospace" fz={size} fw={600} lh={1.1} c="dimmed">–</Text>;
  const [number, suffix] = splitUnit(value(n, unit));
  return <Text ff="monospace" fz={size} fw={600} lh={1.1} style={{ fontVariantNumeric: "tabular-nums" }}>
    {number}{suffix && <Text span ff="monospace" fz="0.6em" fw={500} c="dimmed" ml={3}>{suffix}</Text>}
  </Text>;
}

function splitUnit(text: string): [string, string] {
  const m = /^([$]?[\d.,]+)\s*(.*)$/.exec(text);
  return m ? [m[1], m[2]] : [text, ""];
}

function Stat({ frame, trend, title }: { frame: Frame; trend?: (number | null)[]; title: string }) {
  return <Stack gap={4} style={{ flex: 1 }}>
    <Measure n={frame.rows[0]?.[0] ?? null} unit={frame.columns[0]?.unit} />
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
function CellView({ v, column }: { v: Cell; column: Column }) {
  if ((column.name === "label" || column.name === "state") && typeof v === "string" && v) return <StatusBadge tone={toneOf(v)}>{v}</StatusBadge>;
  const text = cell(v, column);
  if (column.type === "time" && typeof v === "string") return <Text span fz="inherit" title={exact(v)}>{text}</Text>;
  if (column.type === "number") return <Text span ff="monospace" fz="inherit">{text}</Text>;
  return <>{text}</>;
}

function DataTable({ frame, source, act }: { frame: Frame; source?: string; act?: Act }) {
  const { local } = useHost();
  const [sorting, setSorting] = useState<SortingState>([]);
  const data = useMemo(() => frame.rows.map(r => rowOf(frame.columns, r)), [frame]);
  const columns = useMemo<ColumnDef<typeof features, Row>[]>(() => frame.columns.filter(c => !c.hidden).map(c => ({
    id: c.name, accessorFn: (r: Row) => r[c.name], header: label(c.name),
    sortFn: c.type === "number" ? "basic" : "alphanumeric", sortDescFirst: c.type === "number",
    cell: ({ row }: { row: { original: Row } }) => <CellView v={row.original[c.name]} column={c} />,
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
  const done = items.filter(r => r.kind !== "waiting");
  const item = (r: Row) => {
    const waiting = r.kind === "waiting";
    const why = waiting ? [r.reason || "waiting", r.tool, r.detail].filter(Boolean).join(" · ") : String(r.summary || r.kind || "");
    const kinds: ActionKind[] = act ? [...(Number(r.session_id) > 0 ? ["jump" as const] : []), ...(!waiting && !r.ack_pending ? ["ack" as const] : [])] : [];
    return <Paper key={String(r.id)} p="xs" radius="sm" withBorder {...rowProps(kinds, r, act)} className={`row-hover ${rowProps(kinds, r, act).className ?? ""}`} style={{ background: "var(--driga-inset)", borderLeft: waiting ? "3px solid var(--mantine-color-warning-filled)" : undefined }}>
      <Group justify="space-between" wrap="nowrap" align="flex-start" gap="sm">
        <Stack gap={2} style={{ minWidth: 0 }}>
          <Group gap={6} wrap="nowrap">
            <StatusBadge tone={waiting ? "warning" : "neutral"}>{waiting ? `waiting ${typeof r.waited === "number" ? duration(r.waited) : ""}`.trim() : "finished"}</StatusBadge>
            <Text size="sm" fw={500} truncate>{String(r.title || r.summary || "Session " + r.session_id)}</Text>
          </Group>
          <Text size="xs" ff="monospace" c="dimmed" style={{ overflowWrap: "anywhere" }}>{why}</Text>
          <Text size="xs" c="dimmed">{[r.project, r.agent, r.machine].filter(Boolean).join(" · ")}</Text>
        </Stack>
        {act && <RowMenu kinds={kinds} row={r} act={act} label={rowLabel(r)} />}
      </Group>
    </Paper>;
  };
  const section = (title: string, n: number, children: ReactNode) => n > 0 && <Stack gap={6}>
    <Text size="xs" fw={600} c="dimmed">{title} · {n}</Text>{children}
  </Stack>;
  return <ScrollArea style={{ flex: 1 }} type="auto"><Stack gap="sm">
    {section("Permission prompts", prompts?.length ?? 0, prompts?.map(p => <PromptItem key={p.id} prompt={p} answer={answer} />))}
    {section("Needs you", waits.length, waits.map(item))}
    {section("Finished", done.length, done.map(item))}
  </Stack></ScrollArea>;
}

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
