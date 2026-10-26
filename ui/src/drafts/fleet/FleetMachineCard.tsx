import "./Fleet.css";
import { Badge, Card, ColorSwatch, Group, Progress, Stack, Text, VisuallyHidden } from "@mantine/core";
import { Desktop, HardDrives, Laptop, type Icon } from "@phosphor-icons/react";
import { bytes, exact, relative } from "../../panels/format";
import { Measure } from "../../panels/PanelView";
import { plural, type FleetMachineGroup } from "./FleetModel";
import { stateOrder, states, toneVar } from "./FleetTypes";

const icons: Record<FleetMachineGroup["machine"]["kind"], Icon> = { desktop: Desktop, laptop: Laptop, server: HardDrives };

/** Minutes without contact after which "last seen" reads as a problem. */
const staleAfter = 10 * 60_000;

/** FleetMachineCard is one machine in the strip: hostname and reachability,
 * memory used of total, sessions by state as a stacked bar with counts,
 * and what is left over. With onSelect it is a toggle button that filters
 * the view to the machine. An unreachable machine stays, with a hollow dot
 * and when it was last seen. */
export function FleetMachineCard({ group, selected, onSelect }: { group: FleetMachineGroup; selected?: boolean; onSelect?: (id: string) => void }) {
  const { machine: m, counts: c } = group;
  const Glyph = icons[m.kind];
  const total = stateOrder.reduce((n, s) => n + c[s], 0);
  const used = Math.round((m.memoryUsed / m.memoryTotal) * 100);
  const stale = !m.reachable && Date.now() - Date.parse(m.lastSeen) > staleAfter;
  const reach = m.reachable ? "reachable" : `last seen ${relative(m.lastSeen)}`;
  // The card's text is its accessible name: hostname first, then the rest.
  const body = <Stack gap={6}>
    <Group gap={6} wrap="nowrap" justify="space-between">
      <Group gap={6} wrap="nowrap" style={{ minWidth: 0 }}>
        <Glyph size={16} aria-hidden />
        <Text ff="monospace" fz="sm" fw={600} truncate title={m.name}>{m.name}</Text>
        {m.local && <Badge size="xs" variant="default" style={{ flex: "none" }}>local</Badge>}
      </Group>
      <Group gap={4} wrap="nowrap" style={{ flex: "none" }}>
        <ColorSwatch size={7} color={m.reachable ? "var(--mantine-color-success-filled)" : "transparent"} withShadow={false} aria-hidden style={{ border: m.reachable ? undefined : "1px solid var(--driga-muted)" }} />
        <Text fz="xs" ff="monospace" c={stale ? "danger" : "dimmed"} title={exact(m.lastSeen)}>{reach}</Text>
      </Group>
    </Group>
    <Group gap={8} wrap="nowrap" align="baseline">
      <Measure n={m.memoryUsed} unit="bytes" total={m.memoryTotal} size={16} />
      <Text ff="monospace" fz="xs" c="dimmed">{used}%</Text>
      {!m.reachable && <Text ff="monospace" fz="xs" c="dimmed">last known</Text>}
    </Group>
    <Progress value={used} size={4} color="var(--driga-control-border)" aria-label={`Memory used ${used}%`} />
    <Progress.Root size={6} aria-hidden>
      {stateOrder.map(s => c[s] > 0 && <Progress.Section key={s} value={(c[s] / total) * 100} color={toneVar(states[s].tone)} aria-label={`${c[s]} ${states[s].label}`} />)}
    </Progress.Root>
    <Group gap={10} wrap="nowrap">
      {stateOrder.map(s => <Group key={s} gap={3} wrap="nowrap">
        <ColorSwatch size={6} color={toneVar(states[s].tone)} withShadow={false} aria-hidden />
        <Text span ff="monospace" fz="xs" c={c[s] ? undefined : "dimmed"}>{c[s]}<VisuallyHidden> {states[s].label}</VisuallyHidden></Text>
      </Group>)}
    </Group>
    <Text fz="xs" ff="monospace" c="dimmed">{m.leftover ? `${plural(m.leftover.processes, "leftover")} · ${bytes(m.leftover.memory)} reclaimable` : "no leftovers"}</Text>
  </Stack>;
  if (!onSelect) return <Card w={280} padding="sm">{body}</Card>;
  return <Card component="button" type="button" w={280} padding="sm" className="fleet-card" aria-pressed={!!selected} onClick={() => onSelect(m.id)}>{body}</Card>;
}
