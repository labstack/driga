// Gallery lays every widget draft on one board as Home would: the KPI strip,
// the memory chart beside its attribution, the fleet's machine cards, the
// inbox beside the sessions, the activity heatmap beside the swimlanes, and
// the smaller parts in a last row, so the owner sees them together and in
// proportion. It is a composition of the drafts with typed props in and
// callbacks out; the story supplies the fixtures.
import { Badge, Box, Group, Paper, SimpleGrid, Stack, Text, Title, type BoxProps } from "@mantine/core";
import { useId, type ReactNode } from "react";
import { AgentChip } from "./AgentChip";
import { BarGauge, type BarGaugeProps } from "./BarGauge";
import { BarList, type BarListProps } from "./BarList";
import { CategoryBar, type CategoryBarProps } from "./CategoryBar";
import { ChartCard, type ChartCardProps } from "./ChartCard";
import { Donut, type DonutProps } from "./Donut";
import { EmptyState } from "./EmptyState";
import { Heatmap, type HeatmapProps } from "./Heatmap";
import { InboxItem, type InboxEntry, type InboxKind } from "./InboxItem";
import { KpiTile, type KpiTileProps } from "./KpiTile";
import { MachineCard, type Machine } from "./MachineCard";
import { RingGauge, type RingGaugeProps } from "./RingGauge";
import { SessionCard, type Session } from "./SessionCard";
import { SessionSwimlanes, type SessionSwimlanesProps } from "./SessionSwimlanes";
import { StatusBadge } from "./StatusBadge";
import { TrackerBar, type TrackerBarProps } from "./TrackerBar";
import { stateOrder, type Agent } from "./types";

export type GalleryProps = {
  tiles: KpiTileProps[];
  chart: Omit<ChartCardProps, "onRange" | "onAction">;
  donut: DonutProps;
  machines: Machine[];
  inbox: InboxEntry[];
  sessions: Session[];
  heatmap: HeatmapProps;
  swimlanes: SessionSwimlanesProps;
  ranking: BarListProps;
  split: CategoryBarProps;
  tracker: TrackerBarProps;
  rings: RingGaugeProps[];
  gauge: BarGaugeProps;
  checkedAt: string;
  onTile?: (label: string) => void;
  onRange?: ChartCardProps["onRange"];
  onChartAction?: ChartCardProps["onAction"];
  onOpenEntry?: (entry: InboxEntry) => void;
  onApproveEntry?: (entry: InboxEntry) => void;
  onDismissEntry?: (entry: InboxEntry) => void;
  onOpenSession?: (session: Session) => void;
  onApproveSession?: (session: Session) => void;
  onOpenMachine?: (machine: Machine) => void;
  onReclaim?: (machine: Machine) => void;
  onSelectRank?: (id: string) => void;
};

const agents: Agent[] = ["claude", "codex", "gemini", "copilot", "agy"];

const groups: { title: string; kinds: InboxKind[] }[] = [
  { title: "Needs you", kinds: ["permission", "question"] },
  { title: "Stuck", kinds: ["stuck"] },
  { title: "Machines", kinds: ["machine"] },
  { title: "To review", kinds: ["finished"] },
];

/** Panel is the board's frame around a widget: a 36 px header with the
 * title, an optional count and a mono caption, over the widget. */
function Panel({ title, count, caption, span, children, ...box }: { title: string; count?: number; caption?: string; span: number; children: ReactNode } & BoxProps) {
  const id = useId();
  return <Paper component="section" withBorder radius="md" p="sm" aria-labelledby={id} style={{ gridColumn: `span ${span}`, minWidth: 0, display: "flex", flexDirection: "column", gap: 8 }} {...box}>
    <Group justify="space-between" wrap="nowrap" gap="sm" h={24}>
      <Group gap={6} wrap="nowrap" style={{ minWidth: 0 }}>
        <Title order={3} id={id} fz="sm" fw={600} lh={1.2} lineClamp={1} title={title} style={{ letterSpacing: "-0.01em" }}>{title}</Title>
        {typeof count === "number" && <Badge variant="default" size="sm" ff="monospace" fw={500} styles={{ root: { textTransform: "none" } }}>{count}</Badge>}
      </Group>
      {caption && <Text size="xs" c="dimmed" ff="monospace" truncate title={caption} style={{ minWidth: 0 }}>{caption}</Text>}
    </Group>
    {children}
  </Paper>;
}

export function Gallery(p: GalleryProps) {
  const row = (span: number) => ({ gridColumn: `span ${span}`, minWidth: 0 });
  const sessions = [...p.sessions].sort((a, b) => stateOrder.indexOf(a.state) - stateOrder.indexOf(b.state));
  const needs = p.inbox.filter(e => e.kind === "permission" || e.kind === "question" || e.kind === "stuck" || e.kind === "machine").length;
  return <Stack gap="sm">
    <SimpleGrid cols={{ base: 2, md: 3, lg: 6 }} spacing="sm">
      {p.tiles.map(t => <KpiTile key={t.label} {...t} onClick={p.onTile ? () => p.onTile?.(t.label) : undefined} />)}
    </SimpleGrid>
    <Box style={{ display: "grid", gridTemplateColumns: "repeat(12, minmax(0, 1fr))", gap: 12, gridAutoFlow: "row dense" }}>
      <Box style={row(8)}><ChartCard {...p.chart} onRange={p.onRange ?? (() => undefined)} onAction={p.onChartAction} /></Box>
      <Panel title="Memory attribution" caption="fleet · now" span={4}>
        <Box style={{ flex: 1, display: "flex", alignItems: "center" }}><Donut {...p.donut} /></Box>
      </Panel>
      {p.machines.map(m => <Box key={m.name} style={row(4)}><MachineCard machine={m} onOpen={p.onOpenMachine} onReclaim={p.onReclaim} /></Box>)}
      <Panel title="Inbox" count={p.inbox.length} caption={needs ? `${needs} need you` : "nothing needs you"} span={7}>
        {p.inbox.length === 0
          ? <EmptyState kind="all-clear" title="Nothing needs you" caption={`${p.sessions.filter(s => s.state === "working").length} sessions working`} checkedAt={p.checkedAt} />
          : <Stack gap="sm">{groups.map(g => {
            const entries = p.inbox.filter(e => g.kinds.includes(e.kind));
            return entries.length > 0 && <Stack key={g.title} gap={4}>
              <Text size="xs" fw={500} tt="uppercase" lts="0.04em" c="dimmed" lh={1.2}>{g.title} · {entries.length}</Text>
              {entries.map(e => <InboxItem key={e.id} entry={e} onOpen={p.onOpenEntry} onApprove={p.onApproveEntry} onDismiss={p.onDismissEntry} />)}
            </Stack>;
          })}</Stack>}
      </Panel>
      <Panel title="Sessions" count={p.sessions.length} caption="by state" span={5}>
        <Stack gap="xs">
          {sessions.slice(0, 2).map(s => <SessionCard key={s.id} session={s} onOpen={p.onOpenSession} onApprove={p.onApproveSession} />)}
          {sessions.slice(2).map(s => <SessionCard key={s.id} session={s} compact onOpen={p.onOpenSession} onApprove={p.onApproveSession} />)}
        </Stack>
      </Panel>
      <Panel title="Activity" caption="projects × hour · last 24h" span={6}><Heatmap {...p.heatmap} /></Panel>
      <Panel title="Session timeline" caption="last 3h" span={6}><SessionSwimlanes {...p.swimlanes} /></Panel>
      <Panel title={p.ranking.label} span={4}><BarList {...p.ranking} onSelect={p.onSelectRank} /></Panel>
      <Panel title="Memory split" caption="studio · by owner" span={4}>
        <Stack gap="md">
          <CategoryBar {...p.split} />
          <Stack gap={4}>
            <Text size="xs" c="dimmed" lh={1.2}>Last 90 turns of the busiest session</Text>
            <TrackerBar {...p.tracker} />
          </Stack>
        </Stack>
      </Panel>
      <Panel title="Pressure" caption="studio" span={4}>
        <Stack gap="md">
          <Group gap="xl">{p.rings.map(r => <RingGauge key={r.label} {...r} />)}</Group>
          <BarGauge {...p.gauge} />
        </Stack>
      </Panel>
      <Panel title="States and agents" span={6}>
        <Stack gap="sm">
          <Group gap="md">{stateOrder.map(s => <StatusBadge key={s} state={s} />)}</Group>
          <Group gap="xs">{stateOrder.map(s => <StatusBadge key={s} state={s} variant="pill" />)}</Group>
          <Group gap="md">{agents.map(a => <AgentChip key={a} agent={a} name waiting={a === "claude"} />)}</Group>
        </Stack>
      </Panel>
      <Panel title="Empty states" span={6}>
        <Stack gap={0}>
          <EmptyState kind="all-clear" title="Nothing needs you" caption="7 sessions working" checkedAt={p.checkedAt} />
          <EmptyState kind="empty" title="No sessions on laptop-02 yet" caption="Start one in a terminal; Driga notices within a few seconds." action={{ label: "Learn how", onClick: () => undefined }} />
          <EmptyState kind="error" title="The service did not answer" command="driga service logs" onRetry={() => undefined} />
        </Stack>
      </Panel>
    </Box>
  </Stack>;
}
