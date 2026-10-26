// SessionCard is one session in two columns: on the left its state and
// title, where it runs (agent chip, project, branch, machine), its last
// tool line in the inset box and a row of counters; on the right a
// sparkline of tool calls per minute in the state's color and the one
// action that fits the state. The footer says when it started and how long
// it has been quiet, in yellow once that is longer than usual. The compact
// form is one row for a Fleet inspector or a dense list.
import { Badge, Box, Button, Group, Paper, Stack, Text, useComputedColorScheme } from "@mantine/core";
import { GitBranch } from "@phosphor-icons/react";
import type { KeyboardEvent, MouseEvent } from "react";
import { chartTheme } from "../panels/charts";
import { bytes, duration, exact, relative } from "../panels/format";
import { AgentChip } from "./AgentChip";
import { Sparkline } from "./Measure";
import { StatusBadge } from "./StatusBadge";
import type { State } from "./types";
import "./widgets.css";

export type Session = {
  id: string;
  title: string;
  state: State;
  inferred?: boolean;
  /** The adapter name: claude, codex, gemini, copilot, agy. */
  agent: string;
  project: string;
  branch: string;
  machine: string;
  /** The last tool call, as the agent wrote it: Edit(internal/x.go). */
  lastTool?: string;
  lastAt: string;
  turns: number;
  files: number;
  uncommitted: number;
  memoryBytes: number;
  startedAt: string;
  /** How long the session has been quiet, in ms, and how long is usual. */
  quietFor?: number;
  quietExpected?: number;
  /** Tool calls per minute over the last 30 minutes. */
  trend?: (number | null)[];
};

export type SessionCardProps = {
  session: Session;
  compact?: boolean;
  selected?: boolean;
  onOpen?: (session: Session) => void;
  /** Offered while the session waits on the person. */
  onApprove?: (session: Session) => void;
};

export function SessionCard({ session: s, compact, selected, onOpen, onApprove }: SessionCardProps) {
  const scheme = useComputedColorScheme("dark");
  const now = Date.now();
  const line = chartTheme(scheme).states[s.state];
  const overdue = typeof s.quietFor === "number" && typeof s.quietExpected === "number" && s.quietFor > s.quietExpected;
  const open = () => onOpen?.(s);
  const onClick = (ev: MouseEvent<HTMLDivElement>) => { if (!(ev.target as HTMLElement).closest("button, a")) open(); };
  const onKey = (ev: KeyboardEvent<HTMLDivElement>) => { if (ev.key === "Enter" && ev.target === ev.currentTarget) { ev.preventDefault(); open(); } };
  const counter = (text: string, tone?: "warning") => <Badge key={text} size="sm" variant={tone ? "light" : "default"} color={tone} ff="monospace" fw={500} styles={{ root: { textTransform: "none", background: tone ? `var(--driga-tint-${tone})` : undefined } }}
    leftSection={tone ? <Box aria-hidden style={{ width: 5, height: 5, borderRadius: "50%", background: `var(--mantine-color-${tone}-filled)` }} /> : undefined}>{text}</Badge>;
  const counters = [counter(`${s.turns} turns`), counter(`${s.files} files`), s.uncommitted > 0 ? counter(`${s.uncommitted} uncommitted`, "warning") : null, counter(bytes(s.memoryBytes))];
  const where = <Group gap={8} wrap="nowrap" style={{ minWidth: 0 }}>
    <AgentChip agent={s.agent} size={16} waiting={s.state === "waiting"} />
    <Text size="xs" c="dimmed" ff="monospace" lh={1.2} truncate title={`${s.project} · ${s.branch} · ${s.machine}`} style={{ minWidth: 0 }}>
      {s.project} · <GitBranch size={11} aria-hidden style={{ verticalAlign: -1 }} /> {s.branch} · {s.machine}
    </Text>
  </Group>;
  const action = s.state === "waiting" && onApprove
    ? <Button size="compact-xs" variant="light" onClick={() => onApprove(s)}>Approve</Button>
    : onOpen && <Button size="compact-xs" variant="default" onClick={open}>Open terminal</Button>;
  const root = { className: "w-row w-card", "data-clickable": onOpen ? "" : undefined, "data-selected": selected ? "true" : undefined, tabIndex: onOpen ? 0 : undefined, onClick: onOpen ? onClick : undefined, onKeyDown: onOpen ? onKey : undefined };
  // Compact: two lines, the title and its action, then where it runs and
  // the counters, which wrap rather than squeeze the title.
  if (compact) return <Paper withBorder radius="md" px="sm" py={8} {...root}>
    <Group gap="sm" wrap="nowrap" justify="space-between">
      <Group gap={8} wrap="nowrap" style={{ flex: 1, minWidth: 0 }}>
        <Box style={{ flex: "none" }}><StatusBadge state={s.state} inferred={s.inferred} /></Box>
        <Text size="md" fw={600} lh={1.2} truncate title={s.title} style={{ minWidth: 0 }}>{s.title}</Text>
      </Group>
      {action && <Box className="w-actions" style={{ flex: "none" }}>{action}</Box>}
    </Group>
    <Group gap="sm" wrap="wrap" mt={4} justify="space-between">
      {where}
      <Group gap={4} wrap="wrap">{counters}</Group>
    </Group>
  </Paper>;
  return <Paper withBorder radius="md" p="sm" {...root}>
    <Group gap="md" wrap="nowrap" align="stretch">
      <Stack gap={6} style={{ flex: 1, minWidth: 0 }}>
        <Group gap={8} wrap="nowrap" style={{ minWidth: 0 }}>
          <StatusBadge state={s.state} inferred={s.inferred} pulse />
          <Text size="md" fw={600} lh={1.2} truncate title={s.title} style={{ minWidth: 0 }}>{s.title}</Text>
        </Group>
        {where}
        {s.lastTool && <Text size="xs" ff="monospace" className="w-inset" lh={1.4} truncate title={`${s.lastTool} · ${relative(s.lastAt, now)}`} style={{ alignSelf: "flex-start", maxWidth: "100%" }}>
          {s.lastTool} <Text span c="dimmed" ff="monospace" fz="inherit" title={exact(s.lastAt)}>· {relative(s.lastAt, now)}</Text>
        </Text>}
        <Group gap={4} wrap="wrap">{counters}</Group>
      </Stack>
      <Stack gap={6} w={140} align="stretch" justify="space-between" style={{ flex: "none" }}>
        <Box h={36} w={120} style={{ alignSelf: "flex-end" }} title="Tool calls per minute, last 30 minutes">
          {s.trend && s.trend.length > 1 ? <Sparkline window="30 minutes" trend={s.trend} title={`${s.title}: tool calls per minute`} color={line} /> : <Text size="xs" c="dimmed" ta="right">no calls yet</Text>}
        </Box>
        {action && <Box style={{ alignSelf: "flex-end" }}>{action}</Box>}
      </Stack>
    </Group>
    <Group gap={6} mt={8} wrap="nowrap">
      <Text size="xs" c="dimmed" ff="monospace" lh={1.2} title={exact(s.startedAt)}>started {relative(s.startedAt, now)}</Text>
      {typeof s.quietFor === "number" && <Group gap={4} wrap="nowrap">
        <Text size="xs" c="dimmed" ff="monospace" lh={1.2}>·</Text>
        {overdue && <Box aria-hidden style={{ width: 6, height: 6, borderRadius: "50%", background: "var(--mantine-color-warning-filled)" }} />}
        <Text size="xs" c={overdue ? undefined : "dimmed"} ff="monospace" lh={1.2} title={overdue ? `Quiet longer than the usual ${duration(s.quietExpected ?? 0)}` : undefined}>quiet {duration(s.quietFor)}</Text>
      </Group>}
    </Group>
  </Paper>;
}
