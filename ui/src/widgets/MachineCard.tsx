// MachineCard is one machine as an instrument: its name and how Driga
// reaches it, memory as a bar gauge, its sessions as dot counts by state,
// the last 90 minutes as a strip of the worst state in each (when Driga has
// them) and what is left over to reclaim. An unreachable machine keeps its
// card, with a hollow dot and when it was last seen. On Home it opens the
// machine; in the Fleet strip it is a toggle that narrows the view to it.
import { Badge, Box, Button, Group, Paper, Stack, Text } from "@mantine/core";
import { Broom, Desktop, HardDrives, Laptop } from "@phosphor-icons/react";
import type { KeyboardEvent, MouseEvent } from "react";
import { bytes, duration, exact } from "../panels/format";
import { RingGauge } from "./RingGauge";
import { BarGauge } from "./BarGauge";
import { stateVar } from "./StatusBadge";
import { TrackerBar, type TrackerCell } from "./TrackerBar";
import { liveStates, stateWord, type LiveState } from "./types";
import "./widgets.css";

export type Machine = {
  id?: string;
  name: string;
  kind?: "desktop" | "laptop" | "server";
  /** The machine the app runs on. */
  local?: boolean;
  /** Reached over mTLS rather than locally. */
  tls?: boolean;
  reachable: boolean;
  lastSeen: string;
  cpuPercent?: number;
  diskFree?: number;
  diskTotal?: number;
  memUsed: number;
  memTotal: number;
  sessions: Record<LiveState, number>;
  /** The worst state in each of the last polls or minutes, oldest first, with the axis ends. */
  strip?: { cells: TrackerCell[]; from: string; to: string; label: string };
  leftover: { trees: number; bytes: number };
};

export type MachineCardProps = {
  machine: Machine;
  selected?: boolean;
  /** Makes the card a toggle, as in the Fleet strip; it then offers no other action. */
  onSelect?: (machine: Machine) => void;
  onOpen?: (machine: Machine) => void;
  onReclaim?: (machine: Machine) => void;
};

const icons = { desktop: Desktop, laptop: Laptop, server: HardDrives } as const;

export function MachineCard({ machine: m, selected, onSelect, onOpen, onReclaim }: MachineCardProps) {
  const now = Date.now();
  const Glyph = icons[m.kind ?? "desktop"];
  const sinceSeen = now - Date.parse(m.lastSeen);
  const late = !m.reachable && sinceSeen > 10 * 60_000;
  const open = () => onOpen?.(m);
  const onClick = (ev: MouseEvent<HTMLDivElement>) => { if (!(ev.target as HTMLElement).closest("button, a")) open(); };
  const onKey = (ev: KeyboardEvent<HTMLDivElement>) => { if (ev.key === "Enter" && ev.target === ev.currentTarget) { ev.preventDefault(); open(); } };
  const leftover = m.leftover.trees === 0 ? "nothing left over" : `${m.leftover.trees} leftover · ${bytes(m.leftover.bytes)} reclaimable`;
  const body = <Stack gap={8}>
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
    {m.cpuPercent !== undefined && <Stack gap={4}><Text size="xs" c="dimmed">CPU</Text><BarGauge value={m.cpuPercent} max={100} format={n => `${Math.round(n)}%`} label={`CPU on ${m.name}`} /></Stack>}
    {m.diskTotal && m.diskFree !== undefined ? <Group gap="sm" wrap="wrap" role="group" aria-label="Pressure">
      <RingGauge value={m.memUsed} max={m.memTotal} format={bytes} label="Memory" caption={bytes(m.memUsed)} size={36} />
      <RingGauge value={Math.max(0, m.diskTotal - m.diskFree)} max={m.diskTotal} format={bytes} label="Disk" caption={bytes(m.diskTotal - m.diskFree)} size={36} />
    </Group> : <BarGauge value={m.memUsed} max={m.memTotal} format={bytes} label={`Memory on ${m.name}`} />}
    <Group gap={10} wrap="nowrap" role="group" aria-label={liveStates.map(k => `${m.sessions[k]} ${stateWord[k]}`).join(", ")}>
      {liveStates.map(k => <Group key={k} gap={3} wrap="nowrap" title={stateWord[k]}>
        <Box aria-hidden style={{ width: 6, height: 6, borderRadius: "50%", background: m.sessions[k] > 0 ? stateVar(k) : "transparent", boxShadow: m.sessions[k] > 0 ? undefined : `inset 0 0 0 1.5px ${stateVar(k)}`, opacity: m.sessions[k] > 0 ? 1 : 0.5 }} />
        <Text size="xs" ff="monospace" lh={1} c={m.sessions[k] > 0 ? undefined : "dimmed"}>{m.sessions[k]}</Text>
      </Group>)}
      {!m.reachable && <Text size="xs" ff="monospace" c="dimmed" lh={1}>last known</Text>}
    </Group>
    {m.strip && <TrackerBar cells={m.strip.cells} label={m.strip.label} from={m.strip.from} to={m.strip.to} height={10} />}
    <Group justify="space-between" wrap="nowrap" gap="xs">
      <Text size="xs" c="dimmed" ff="monospace" lh={1.2} truncate title={leftover} style={{ minWidth: 0 }}>{leftover}</Text>
      {!onSelect && onReclaim && m.leftover.trees > 0 && <Button size="compact-xs" variant="subtle" color="gray" leftSection={<Broom size={12} />} onClick={() => onReclaim(m)} style={{ flex: "none" }}>Reclaim</Button>}
    </Group>
  </Stack>;
  const dashed = m.reachable ? undefined : "dashed";
  // A toggle is a real button; its text is its name, hostname first.
  if (onSelect) return <Paper component="button" type="button" withBorder radius="md" p="sm" className="w-row w-card" data-clickable="" aria-pressed={!!selected} onClick={() => onSelect(m)} style={{ borderStyle: dashed, textAlign: "left", width: "100%", cursor: "pointer" }}>{body}</Paper>;
  return <Paper withBorder radius="md" p="sm" className="w-row w-card" data-clickable={onOpen ? "" : undefined} data-selected={selected ? "true" : undefined} tabIndex={onOpen ? 0 : undefined} onClick={onOpen ? onClick : undefined} onKeyDown={onOpen ? onKey : undefined} style={{ borderStyle: dashed }}>{body}</Paper>;
}
