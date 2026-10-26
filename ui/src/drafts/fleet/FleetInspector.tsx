import "./Fleet.css";
import { Alert, Badge, Button, Card, ColorSwatch, Group, Progress, ScrollArea, Stack, Text, Title, UnstyledButton } from "@mantine/core";
import { Desktop, GitBranch, HandPalm, HardDrives, Laptop, type Icon } from "@phosphor-icons/react";
import { bytes, duration, exact, relative } from "../../panels/format";
import { Measure } from "../../panels/PanelView";
import { StatusBadge } from "../../panels/StatusBadge";
import { childrenOf, groupFleet, machineOf, machinesOfProject, overlapsOf, plural, projectOf, type FleetMachineGroup } from "./FleetModel";
import { FleetTurnStrip } from "./FleetTurnStrip";
import { agents, stateOrder, states, toneVar, type FleetActions, type FleetData, type FleetMachine, type FleetProject, type FleetSelection, type FleetSession } from "./FleetTypes";

const icons: Record<FleetMachine["kind"], Icon> = { desktop: Desktop, laptop: Laptop, server: HardDrives };

/** FleetInspector is the panel beside either view: the selected session with
 * its files, subagents and actions, the selected machine with its projects
 * and leftovers, or the selected project with the machines it runs on.
 * Nothing opens in a modal; the panel stays, empty, until something is
 * selected. Actions report through callbacks, as the Inbox's do. */
export function FleetInspector({ data, selection, onSelect, onOpen, onReply, width = 320 }: FleetActions & { data: FleetData; selection: FleetSelection | null; width?: number }) {
  const found = selection && (selection.kind === "session" ? data.sessions.find(s => s.id === selection.id) : selection.kind === "machine" ? data.machines.find(m => m.id === selection.id) : data.projects.find(p => p.id === selection.id));
  // A labelled group, not a landmark: the panel belongs to the view, and two
  // views on one page would otherwise collide.
  return <Card role="group" aria-label="Inspector" w={width} padding="sm" style={{ flex: "none", display: "flex", flexDirection: "column", minHeight: 0 }}>
    <ScrollArea type="auto" scrollbarSize={4} style={{ flex: 1, minHeight: 0 }}>
      {!selection || !found
        ? <Nothing />
        : selection.kind === "session" ? <SessionPanel session={found as FleetSession} data={data} onSelect={onSelect} onOpen={onOpen} onReply={onReply} />
        : selection.kind === "machine" ? <MachinePanel group={groupFleet(data).find(g => g.machine.id === selection.id)!} onSelect={onSelect} />
        : <ProjectPanel project={found as FleetProject} data={data} onSelect={onSelect} />}
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
  return <UnstyledButton className="fleet-row-title" onClick={() => onSelect({ kind: "session", id: s.id })} px={4} py={2}>
    <Group gap={6} wrap="nowrap">
      <ColorSwatch size={6} color={toneVar(states[s.state].tone)} withShadow={false} aria-hidden style={{ flex: "none" }} />
      <Text fz="xs" truncate title={s.title}>{s.title}</Text>
      <Text fz="xs" c="dimmed" ff="monospace" style={{ flex: "none" }}>{extra ?? states[s.state].label}</Text>
    </Group>
  </UnstyledButton>;
}

function SessionPanel({ session: s, data, onSelect, onOpen, onReply }: FleetActions & { session: FleetSession; data: FleetData }) {
  const project = projectOf(data.projects, s.project);
  const machine = machineOf(data.machines, s.machine);
  const children = childrenOf(data.sessions, s.id);
  const parent = s.parent ? data.sessions.find(x => x.id === s.parent) : undefined;
  const shared = overlapsOf(data.sessions, s.id);
  const sharedBy = (file: string) => shared.filter(o => o.files.includes(file)).map(o => o.with);
  const quiet = Math.max(0, Date.now() - Date.parse(s.lastActivity));
  return <Stack gap="sm">
    <Group gap="xs" wrap="nowrap" justify="space-between">
      <StatusBadge tone={states[s.state].tone} inferred={s.inferred}>{states[s.state].label}</StatusBadge>
      <Text ff="monospace" fz="xs" c="dimmed">#{s.id}</Text>
    </Group>
    <Title order={3} fz="md" lh={1.3} style={{ textWrap: "balance", letterSpacing: "-0.01em" }}>{s.title}</Title>
    <Group gap={8} wrap="wrap">
      <Badge variant="default" size="xs">{agents[s.agent]}</Badge>
      <Text ff="monospace" fz="xs" c="dimmed">{project?.name ?? s.project}</Text>
      {s.branch && <Group gap={3} wrap="nowrap"><GitBranch size={12} aria-hidden /><Text ff="monospace" fz="xs" c="dimmed">{s.branch}</Text></Group>}
      <Text ff="monospace" fz="xs" c="dimmed">{machine?.name ?? s.machine}</Text>
    </Group>
    {s.waiting && <Alert color="warning" variant="light" icon={<HandPalm size={16} />} title="Needs you" p="xs">{s.waiting}</Alert>}
    {machine && !machine.reachable && <Text fz="xs" c="dimmed">Last known: {machine.name} was last seen {relative(machine.lastSeen)}.</Text>}
    {s.lastTool && <div className="fleet-inset"><Text ff="monospace" fz="xs" truncate title={`${s.lastTool} · ${relative(s.lastActivity)}`}>{s.lastTool} · {relative(s.lastActivity)}</Text></div>}
    <Group gap={6}>
      <Badge variant="default" size="xs" ff="monospace">{plural(s.turns.length, "turn")}</Badge>
      <Badge variant="default" size="xs" ff="monospace">{plural(s.files.length, "file")}</Badge>
      <Badge variant="default" size="xs" ff="monospace">{bytes(s.memory)}</Badge>
    </Group>
    <Stack gap={4}><FleetTurnStrip session={s} /><Text fz="xs" c="dimmed" ff="monospace">last 30 turns</Text></Stack>
    {s.files.length > 0 && <Stack gap={2}>
      <Label>Files</Label>
      {s.files.map(f => {
        const others = sharedBy(f);
        return <Group key={f} gap={6} wrap="nowrap" justify="space-between">
          <Text ff="monospace" fz="xs" truncate title={f}>{f}</Text>
          {others.length > 0 && <Badge color="danger" variant="light" size="xs" style={{ flex: "none" }} title={`Also edited by ${others.map(o => o.title).join(", ")}`}>also #{others.map(o => o.id).join(", #")}</Badge>}
        </Group>;
      })}
    </Stack>}
    {children.length > 0 && <Stack gap={2}><Label>Subagents</Label>{children.map(c => <SessionLink key={c.id} session={c} onSelect={onSelect} />)}</Stack>}
    {parent && <Stack gap={2}><Label>Spawned by</Label><SessionLink session={parent} onSelect={onSelect} /></Stack>}
    <Group gap="xs" mt={4}>
      {s.state === "waiting" && <Button onClick={() => onReply(s)}>Reply</Button>}
      <Button variant="default" onClick={() => onOpen(s)}>Open</Button>
    </Group>
    <Text fz="xs" c="dimmed" ff="monospace" title={`Started ${exact(s.started)}`}>started {relative(s.started)} · quiet {duration(quiet)}</Text>
  </Stack>;
}

function MachinePanel({ group, onSelect }: { group: FleetMachineGroup; onSelect: FleetActions["onSelect"] }) {
  const { machine: m, counts: c } = group;
  const Glyph = icons[m.kind];
  const used = Math.round((m.memoryUsed / m.memoryTotal) * 100);
  const total = stateOrder.reduce((n, s) => n + c[s], 0);
  return <Stack gap="sm">
    <Group gap={6} wrap="nowrap">
      <Glyph size={16} aria-hidden />
      <Title order={3} fz="md" ff="monospace" lh={1.3}>{m.name}</Title>
      {m.local && <Badge variant="default" size="xs">local</Badge>}
    </Group>
    <Group gap={4} wrap="nowrap">
      <ColorSwatch size={7} color={m.reachable ? "var(--mantine-color-success-filled)" : "transparent"} withShadow={false} aria-hidden style={{ border: m.reachable ? undefined : "1px solid var(--driga-muted)" }} />
      <Text fz="xs" ff="monospace" c="dimmed" title={exact(m.lastSeen)}>{m.reachable ? "reachable" : `unreachable · last seen ${relative(m.lastSeen)}`}</Text>
    </Group>
    <Stack gap={4}>
      <Label>Memory</Label>
      <Group gap={8} wrap="nowrap" align="baseline"><Measure n={m.memoryUsed} unit="bytes" total={m.memoryTotal} size={22} /><Text ff="monospace" fz="xs" c="dimmed">{used}% used{m.reachable ? "" : ", last known"}</Text></Group>
      <Progress value={used} size={4} color="var(--driga-control-border)" aria-label={`Memory used ${used}%`} />
    </Stack>
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

function ProjectPanel({ project: p, data, onSelect }: { project: FleetProject; data: FleetData; onSelect: FleetActions["onSelect"] }) {
  const machines = machinesOfProject(data, p.id);
  const sessions = data.sessions.filter(s => s.project === p.id);
  return <Stack gap="sm">
    <Title order={3} fz="md" ff="monospace" lh={1.3}>{p.name}</Title>
    <Text ff="monospace" fz="xs" c="dimmed">{p.repo}</Text>
    <Stack gap={2}>
      <Label>Machines</Label>
      <Text fz="xs" ff="monospace">{machines.length ? machines.map(m => m.name).join(" · ") : "none"}</Text>
    </Stack>
    <Stack gap={2}>
      <Label>Sessions</Label>
      {sessions.map(s => <SessionLink key={s.id} session={s} onSelect={onSelect} extra={`${states[s.state].label} · ${machineOf(data.machines, s.machine)?.name ?? s.machine}`} />)}
    </Stack>
  </Stack>;
}
