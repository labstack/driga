import "./Fleet.css";
import { Badge, Button, Card, ColorSwatch, Group, Progress, ScrollArea, Stack, Text, Title, UnstyledButton } from "@mantine/core";
import { Desktop, GitBranch, HandPalm, HardDrives, Laptop, type Icon } from "@phosphor-icons/react";
import { bytes, duration, exact, plural, relative } from "../panels/format";
import { BarGauge } from "../widgets/BarGauge";
import { AgentChip } from "../widgets/AgentChip";
import { CategoryBar } from "../widgets/CategoryBar";
import { Measure } from "../widgets/Measure";
import { StatusBadge, toneVar } from "../widgets/StatusBadge";
import { TrackerBar } from "../widgets/TrackerBar";
import { FleetRelationships, reach, turnCells, turnWords, type FleetMachineGroup } from "./FleetModel";
import { stateOrder, states, type FleetActions, type FleetData, type FleetMachine, type FleetProject, type FleetSelection, type FleetSession } from "./FleetTypes";

const icons: Record<NonNullable<FleetMachine["kind"]>, Icon> = { desktop: Desktop, laptop: Laptop, server: HardDrives };

/** FleetInspector is the panel beside the view: the selected session with
 * its files, subagents and actions, the selected machine with its projects
 * and leftovers, or the selected project with the machines it runs on.
 * Nothing opens in a modal; the panel stays, empty, until something is
 * selected. Actions report through callbacks, as the Inbox's do. What the
 * service does not send yet (turns, files, a start time) is simply absent. */
export function FleetInspector({ data, selection, onSelect, onOpen, onReply, width = 320 }: FleetActions & { data: FleetData; selection: FleetSelection | null; width?: number }) {
  const relationships = new FleetRelationships(data);
  const found = selection && (selection.kind === "session" ? relationships.session(selection.id, selection.machine) : selection.kind === "machine" ? relationships.machine(selection.id) : relationships.project(selection.id));
  // A labelled group, not a landmark: the panel belongs to the view, and two
  // views on one page would otherwise collide.
  return <Card role="group" aria-label="Inspector" w={{ base: "100%", lg: width }} padding="sm" style={{ flex: "none", display: "flex", flexDirection: "column", minHeight: 0 }}>
    <ScrollArea type="auto" scrollbarSize={4} style={{ flex: 1, minHeight: 0 }}>
      {!selection || !found
        ? <Nothing />
        : selection.kind === "session" ? <SessionPanel session={found as FleetSession} relationships={relationships} onSelect={onSelect} onOpen={onOpen} onReply={onReply} />
        : selection.kind === "machine" ? <MachinePanel group={relationships.groups.find(g => g.machine.id === selection.id)!} onSelect={onSelect} />
        : <ProjectPanel project={found as FleetProject} relationships={relationships} onSelect={onSelect} />}
    </ScrollArea>
  </Card>;
}

function Nothing() {
  return <Stack align="center" justify="center" gap={4} py="xl" px="sm">
    <Text fw={600} fz="sm">Nothing selected</Text>
    <Text fz="xs" c="dimmed" ta="center" style={{ textWrap: "pretty" }}>Select a session or a machine to see its details and actions here.</Text>
  </Stack>;
}

function Label({ children }: { children: string }) {
  return <Text fz="xs" fw={600} c="dimmed">{children}</Text>;
}

/** SessionLink names another session, selectable, with its state dot. */
function SessionLink({ session: s, onSelect, extra }: { session: FleetSession; onSelect: FleetActions["onSelect"]; extra?: string }) {
  return <UnstyledButton className="fleet-row-title" onClick={() => onSelect({ kind: "session", id: s.id, machine: s.machine })} px={4} py={2}>
    <Group gap={6} wrap="nowrap">
      <ColorSwatch size={6} color={toneVar(states[s.state].tone)} withShadow={false} aria-hidden style={{ flex: "none" }} />
      <Text fz="xs" truncate title={s.title}>{s.title}</Text>
      <Text fz="xs" c="dimmed" ff="monospace" style={{ flex: "none" }}>{extra ?? s.label ?? states[s.state].label}</Text>
    </Group>
  </UnstyledButton>;
}

function SessionPanel({ session: s, relationships, onSelect, onOpen, onReply }: FleetActions & { session: FleetSession; relationships: FleetRelationships }) {
  const project = relationships.project(s.project);
  const machine = relationships.machine(s.machine);
  const children = relationships.children(s);
  const parent = relationships.parent(s);
  const shared = relationships.overlaps(s);
  const sharedBy = (file: string) => shared.filter(o => o.files.includes(file)).map(o => o.with);
  const quiet = Math.max(0, Date.now() - Date.parse(s.lastActivity));
  return <Stack gap="sm">
    <Group gap="xs" wrap="nowrap" justify="space-between">
      <StatusBadge variant="pill" state={s.state} label={s.label} inferred={s.inferred} />
      <Text ff="monospace" fz="xs" c="dimmed">#{s.id}</Text>
    </Group>
    <Title order={2} fz="md" lh={1.3} style={{ textWrap: "balance", letterSpacing: "-0.01em" }}>{s.title}</Title>
    <Group gap={8} wrap="wrap">
      <AgentChip agent={s.agent} name size={16} />
      <Text ff="monospace" fz="xs" c="dimmed">{project?.name ?? s.project}</Text>
      {s.branch && <Group gap={3} wrap="nowrap"><GitBranch size={12} aria-hidden /><Text ff="monospace" fz="xs" c="dimmed">{s.branch}</Text></Group>}
      <Text ff="monospace" fz="xs" c="dimmed">{machine?.name ?? s.machine}</Text>
    </Group>
    {s.waiting && <Group className="w-needs-you" gap={8} wrap="nowrap" align="flex-start" px="xs" py={6} role="status">
      <HandPalm size={16} aria-hidden style={{ color: "var(--mantine-color-warning-filled)", flex: "none", marginTop: 1 }} />
      <Stack gap={2}><Text fz="xs" fw={600}>Needs you</Text><Text fz="xs" style={{ overflowWrap: "anywhere" }}>{s.waiting}</Text></Stack>
    </Group>}
    {s.evidence && <Text fz="xs" c="dimmed">{s.evidence}</Text>}
    {machine && !machine.reachable && <Text fz="xs" c="dimmed">Last known: {machine.name} was last seen {relative(machine.lastSeen)}.</Text>}
    {s.lastTool && <Text ff="monospace" fz="xs" className="w-inset" truncate title={`${s.lastTool} · ${relative(s.lastActivity)}`} style={{ display: "block" }}>{s.lastTool} · {relative(s.lastActivity)}</Text>}
    <Group gap={6}>
      {!!s.turns?.length && <Badge variant="default" size="xs" ff="monospace">{plural(s.turns.length, "turn")}</Badge>}
      {!!s.files?.length && <Badge variant="default" size="xs" ff="monospace">{plural(s.files.length, "file")}</Badge>}
      <Badge variant="default" size="xs" ff="monospace">{bytes(s.memory)}</Badge>
    </Group>
    {s.turns && s.turns.length > 0 && <TrackerBar cells={turnCells(s)} label="Last 30 turns" summary={turnWords(s)} height={8} to={`turn ${Math.min(30, s.turns.length)}`} from={s.turns.length > 30 ? `turn ${s.turns.length - 29}` : "turn 1"} />}
    {s.files && s.files.length > 0 && <Stack gap={2}>
      <Label>Files</Label>
      {s.files.map(f => {
        const others = sharedBy(f);
        return <Group key={f} gap={6} wrap="nowrap" justify="space-between">
          <Text ff="monospace" fz="xs" truncate title={f}>{f}</Text>
          {others.length > 0 && <Badge color="danger" variant="light" size="xs" style={{ flex: "none", background: "var(--driga-tint-danger)", color: "var(--driga-text)" }} title={`Also edited by ${others.map(o => o.title).join(", ")}`}>also #{others.map(o => o.id).join(", #")}</Badge>}
        </Group>;
      })}
    </Stack>}
    {children.length > 0 && <Stack gap={2}><Label>Subagents</Label>{children.map(c => <SessionLink key={`${c.machine}/${c.id}`} session={c} onSelect={onSelect} />)}</Stack>}
    {parent && <Stack gap={2}><Label>Spawned by</Label><SessionLink session={parent} onSelect={onSelect} /></Stack>}
    <Group gap="xs" mt={4}>
      {s.state === "waiting" && <Button onClick={() => onReply(s)}>Reply</Button>}
      <Button variant="default" onClick={() => onOpen(s)}>Open</Button>
    </Group>
    <Text fz="xs" c="dimmed" ff="monospace" title={s.started ? `Started ${exact(s.started)}` : undefined}>{s.started ? `started ${relative(s.started)} · ` : ""}quiet {duration(quiet)}</Text>
  </Stack>;
}

function MachinePanel({ group, onSelect }: { group: FleetMachineGroup; onSelect: FleetActions["onSelect"] }) {
  const { machine: m, counts: c } = group;
  const Glyph = icons[m.kind ?? "desktop"];
  const used = m.memoryTotal > 0 ? Math.round((m.memoryUsed / m.memoryTotal) * 100) : 0;
  const total = stateOrder.reduce((n, s) => n + c[s], 0);
  return <Stack gap="sm">
    <Group gap={6} wrap="nowrap">
      <Glyph size={16} aria-hidden />
      <Title order={2} fz="md" ff="monospace" lh={1.6}>{m.name}</Title>
      {m.local && <Badge variant="default" size="xs">local</Badge>}
    </Group>
    <Group gap={4} wrap="nowrap">
      <ColorSwatch size={7} color={m.reachable ? "var(--mantine-color-success-filled)" : "transparent"} withShadow={false} aria-hidden style={{ border: m.reachable ? undefined : "1px solid var(--driga-muted)" }} />
      <Text fz="xs" ff="monospace" c="dimmed" title={exact(m.lastSeen)}>{reach(m)}</Text>
    </Group>
    <Stack gap={4}>
      <Label>Memory</Label>
      <Group gap={8} wrap="nowrap" align="baseline"><Measure n={m.memoryUsed} unit="bytes" total={m.memoryTotal} size={22} /><Text ff="monospace" fz="xs" c="dimmed">{used}% used{m.reachable ? "" : ", last known"}</Text></Group>
      <Progress value={used} size={4} color="var(--driga-control-border)" aria-label={`Memory used ${used}%`} />
      {m.memoryByRole && m.memoryByRole.length > 0 && <CategoryBar label={`Agent memory on ${m.name} by owner`} segments={m.memoryByRole} total={m.memoryTotal} format={bytes} size={6} />}
    </Stack>
    {m.cpuPercent !== undefined && <Stack gap={4}><Label>CPU</Label><BarGauge value={m.cpuPercent} max={100} format={n => `${Math.round(n)}%`} label={`CPU on ${m.name}`} /></Stack>}
    {!!m.diskTotal && m.diskFree !== undefined && <Stack gap={4}><Label>Disk</Label><BarGauge value={Math.max(0, m.diskTotal - m.diskFree)} max={m.diskTotal} format={bytes} label={`Disk on ${m.name}`} /></Stack>}
    <Stack gap={2}>
      <Label>Sessions</Label>
      {total === 0 && <Text fz="xs" c="dimmed">None</Text>}
      {stateOrder.filter(s => c[s] > 0).map(s => <Group key={s} gap={6} wrap="nowrap"><ColorSwatch size={6} color={toneVar(states[s].tone)} withShadow={false} aria-hidden /><Text fz="xs" ff="monospace">{c[s]} {states[s].label}</Text></Group>)}
    </Stack>
    {group.projects.length > 0 && <Stack gap={2}>
      <Label>Projects</Label>
      {group.projects.map(p => <UnstyledButton key={p.project.id} className="fleet-row-title" px={4} py={2} onClick={() => onSelect({ kind: "project", id: p.project.id })}>
        <Group gap={6} wrap="nowrap" justify="space-between"><Text fz="xs" ff="monospace">{p.project.name}</Text><Text fz="xs" ff="monospace" c="dimmed">{plural(p.sessions.length, "session")} · {bytes(p.memory)}</Text></Group>
      </UnstyledButton>)}
    </Stack>}
    <Stack gap={2}>
      <Label>Leftover</Label>
      <Text fz="xs" ff="monospace">{m.leftover ? `${plural(m.leftover.processes, "process")} · ${bytes(m.leftover.memory)} reclaimable` : "none"}</Text>
    </Stack>
  </Stack>;
}

function ProjectPanel({ project: p, relationships, onSelect }: { project: FleetProject; relationships: FleetRelationships; onSelect: FleetActions["onSelect"] }) {
  const machines = relationships.projectMachines(p.id);
  const sessions = relationships.projectSessions(p.id);
  return <Stack gap="sm">
    <Title order={2} fz="md" ff="monospace" lh={1.6}>{p.name}</Title>
    {p.repo && <Text ff="monospace" fz="xs" c="dimmed" truncate title={p.repo}>{p.repo}</Text>}
    <Stack gap={2}>
      <Label>Machines</Label>
      <Text fz="xs" ff="monospace">{machines.length ? machines.map(m => m.name).join(" · ") : "none"}</Text>
    </Stack>
    <Stack gap={2}>
      <Label>Sessions</Label>
      {sessions.map(s => <SessionLink key={`${s.machine}/${s.id}`} session={s} onSelect={onSelect} extra={`${s.label ?? states[s.state].label} · ${relationships.machine(s.machine)?.name ?? s.machine}`} />)}
    </Stack>
  </Stack>;
}
