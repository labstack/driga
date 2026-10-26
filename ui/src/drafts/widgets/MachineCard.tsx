// MachineCard is one machine as an instrument: its name and how Driga
// reaches it, memory as a bar gauge, its sessions as a stacked state bar
// with the counts, the last 90 minutes as a strip of the worst state in
// each minute (Zendesk's 90-day bar scaled to minutes) and what is left
// over to reclaim. An unreachable machine keeps its card, with a hollow
// dot and when it was last seen.
import { Badge, Box, Button, Group, Paper, Progress, Stack, Text } from "@mantine/core";
import { Broom, Desktop, HardDrives, Laptop } from "@phosphor-icons/react";
import type { KeyboardEvent, MouseEvent } from "react";
import { bytes, duration, exact } from "../../panels/format";
import { BarGauge } from "./BarGauge";
import { stateVar } from "./palette";
import { TrackerBar, type TrackerCell } from "./TrackerBar";
import { stateWord, type State } from "./types";
import "./widgets.css";

export type LiveState = Exclude<State, "finished">;

export type Machine = {
  name: string;
  kind: "desktop" | "laptop" | "server";
  /** The machine the app runs on. */
  local?: boolean;
  /** Reached over mTLS rather than locally. */
  tls?: boolean;
  reachable: boolean;
  lastSeen: string;
  memUsed: number;
  memTotal: number;
  sessions: Record<LiveState, number>;
  /** The worst state in each of the last 90 minutes, oldest first. */
  strip: TrackerCell[];
  leftover: { trees: number; bytes: number };
};

export type MachineCardProps = {
  machine: Machine;
  selected?: boolean;
  onOpen?: (machine: Machine) => void;
  onReclaim?: (machine: Machine) => void;
};

const icons = { desktop: Desktop, laptop: Laptop, server: HardDrives } as const;
const order: LiveState[] = ["working", "waiting", "stuck", "idle"];

export function MachineCard({ machine: m, selected, onOpen, onReclaim }: MachineCardProps) {
  const now = Date.now();
  const Glyph = icons[m.kind];
  const total = order.reduce((s, k) => s + m.sessions[k], 0);
  const sinceSeen = now - Date.parse(m.lastSeen);
  const late = !m.reachable && sinceSeen > 10 * 60_000;
  const open = () => onOpen?.(m);
  const onClick = (ev: MouseEvent<HTMLDivElement>) => { if (!(ev.target as HTMLElement).closest("button, a")) open(); };
  const onKey = (ev: KeyboardEvent<HTMLDivElement>) => { if (ev.key === "Enter" && ev.target === ev.currentTarget) { ev.preventDefault(); open(); } };
  const leftover = m.leftover.trees === 0 ? "nothing left over" : `${m.leftover.trees} leftover · ${bytes(m.leftover.bytes)} reclaimable`;
  return <Paper withBorder radius="md" p="sm" className="w-row w-card" data-clickable={onOpen ? "" : undefined} data-selected={selected ? "true" : undefined} data-reachable={m.reachable} tabIndex={onOpen ? 0 : undefined} onClick={onOpen ? onClick : undefined} onKeyDown={onOpen ? onKey : undefined} style={{ borderStyle: m.reachable ? undefined : "dashed" }}>
    <Stack gap={8}>
      <Group justify="space-between" wrap="nowrap" gap="sm">
        <Group gap={6} wrap="nowrap" style={{ minWidth: 0 }}>
          <Glyph size={16} aria-hidden style={{ color: "var(--driga-muted)", flex: "none" }} />
          <Text size="md" fw={600} ff="monospace" lh={1.2} truncate title={m.name} style={{ minWidth: 0 }}>{m.name}</Text>
          {m.local && <Badge size="xs" variant="default" fw={500} styles={{ root: { textTransform: "none", flex: "none" } }}>local</Badge>}
          {m.tls && <Badge size="xs" variant="default" fw={500} styles={{ root: { textTransform: "none", flex: "none" } }}>mTLS</Badge>}
        </Group>
        <Group gap={6} wrap="nowrap" style={{ flex: "none" }} title={exact(m.lastSeen)}>
          <Box aria-hidden style={{ width: 6, height: 6, borderRadius: "50%", background: m.reachable ? "var(--mantine-color-success-filled)" : "transparent", boxShadow: m.reachable ? undefined : "inset 0 0 0 1.5px var(--driga-faint)" }} />
          <Text size="xs" lh={1.2} c={late ? "danger" : "dimmed"} fw={late ? 500 : undefined} style={{ whiteSpace: "nowrap" }}>{m.reachable ? "reachable" : `last seen ${duration(sinceSeen)} ago`}</Text>
        </Group>
      </Group>
      <BarGauge value={m.memUsed} max={m.memTotal} format={bytes} label={`Memory on ${m.name}`} />
      <Group gap="sm" wrap="nowrap" align="center">
        <Progress.Root size={6} style={{ flex: 1 }} styles={{ root: { background: "var(--driga-border)" } }}>
          {order.map(k => m.sessions[k] > 0 && <Progress.Section key={k} value={Math.round((m.sessions[k] / total) * 10000) / 100} color={stateVar(k)} aria-label={`${m.sessions[k]} ${stateWord[k]}`} style={{ boxShadow: "inset -1px 0 0 var(--driga-raised)" }} />)}
        </Progress.Root>
        <Group gap={8} wrap="nowrap" style={{ flex: "none" }} aria-label={order.map(k => `${m.sessions[k]} ${stateWord[k]}`).join(", ")}>
          {order.map(k => <Group key={k} gap={3} wrap="nowrap" title={stateWord[k]}>
            <Box aria-hidden style={{ width: 6, height: 6, borderRadius: "50%", background: m.sessions[k] > 0 ? stateVar(k) : "transparent", boxShadow: m.sessions[k] > 0 ? undefined : `inset 0 0 0 1.5px ${stateVar(k)}`, opacity: m.sessions[k] > 0 ? 1 : 0.5 }} />
            <Text size="xs" ff="monospace" lh={1} c={m.sessions[k] > 0 ? undefined : "dimmed"}>{m.sessions[k]}</Text>
          </Group>)}
        </Group>
      </Group>
      <TrackerBar cells={m.strip} label={`${m.name}, last 90 minutes`} from="90m" to="now" height={10} />
      <Group justify="space-between" wrap="nowrap" gap="xs">
        <Text size="xs" c="dimmed" ff="monospace" lh={1.2} truncate title={leftover} style={{ minWidth: 0 }}>{leftover}</Text>
        {onReclaim && m.leftover.trees > 0 && <Button size="compact-xs" variant="subtle" color="gray" leftSection={<Broom size={12} />} onClick={() => onReclaim(m)} style={{ flex: "none" }}>Reclaim</Button>}
      </Group>
    </Stack>
  </Paper>;
}
