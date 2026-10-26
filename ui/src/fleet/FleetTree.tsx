import "./Fleet.css";
import { ActionIcon, Badge, Button, ColorSwatch, Group, Table, Text, UnstyledButton, VisuallyHidden } from "@mantine/core";
import { CaretDown, CaretRight, Desktop, FolderSimple, GitBranch, HardDrives, Laptop, type Icon } from "@phosphor-icons/react";
import { useState } from "react";
import { bytes, exact, plural, relative } from "../panels/format";
import { AgentChip } from "../widgets/AgentChip";
import { StatusBadge } from "../widgets/StatusBadge";
import { childrenOf, groupFleet, roots, type FleetMachineGroup, type FleetProjectGroup } from "./FleetModel";
import type { FleetActions, FleetData, FleetMachine, FleetSelection, FleetSession } from "./FleetTypes";

const icons: Record<NonNullable<FleetMachine["kind"]>, Icon> = { desktop: Desktop, laptop: Laptop, server: HardDrives };

const indent = 20;

/** FleetTree is the Fleet as a table: machine rows that expand into project
 * rows, which hold session rows, with subagents one level deeper. It is the
 * fully accessible mode: every row and action is a real control, and it
 * needs no pointer. Selecting a row fills the inspector. */
export function FleetTree({ data, selected, onSelect, onOpen, onReply }: FleetActions & { data: FleetData; selected: FleetSelection | null }) {
  const groups = groupFleet(data);
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const toggle = (id: string) => setCollapsed(prev => { const next = new Set(prev); if (next.has(id)) next.delete(id); else next.add(id); return next; });
  const is = (kind: FleetSelection["kind"], id: string) => selected?.kind === kind && selected.id === id;
  return <Table.ScrollContainer minWidth={760} type="native">
    <Table aria-label="Fleet tree" layout="fixed">
      <Table.Thead>
        <Table.Tr>
          <Table.Th w={96}>State</Table.Th>
          <Table.Th>Name</Table.Th>
          <Table.Th w={120}>Agent</Table.Th>
          <Table.Th w={134}>Branch</Table.Th>
          <Table.Th w={124} ta="right">Memory</Table.Th>
          <Table.Th w={96} ta="right">Last activity</Table.Th>
          <Table.Th w={120}><VisuallyHidden>Actions</VisuallyHidden></Table.Th>
        </Table.Tr>
      </Table.Thead>
      <Table.Tbody>
        {groups.map(g => {
          const open = !collapsed.has(g.machine.id);
          return [
            <MachineRow key={g.machine.id} group={g} open={open} selected={is("machine", g.machine.id)} onToggle={() => toggle(g.machine.id)} onSelect={onSelect} />,
            ...(open ? g.projects.flatMap(p => [
              <ProjectRow key={`${g.machine.id}/${p.project.id}`} group={p} selected={is("project", p.project.id)} onSelect={onSelect} />,
              ...roots(p.sessions).flatMap(s => [
                <SessionRow key={s.id} session={s} level={2} selected={is("session", s.id)} onSelect={onSelect} onOpen={onOpen} onReply={onReply} />,
                ...childrenOf(data.sessions, s.id).map(c => <SessionRow key={c.id} session={c} level={3} selected={is("session", c.id)} onSelect={onSelect} onOpen={onOpen} onReply={onReply} />),
              ]),
            ]) : []),
          ];
        })}
      </Table.Tbody>
    </Table>
  </Table.ScrollContainer>;
}

function MachineRow({ group: g, open, selected, onToggle, onSelect }: { group: FleetMachineGroup; open: boolean; selected: boolean; onToggle: () => void; onSelect: FleetActions["onSelect"] }) {
  const m = g.machine;
  const Glyph = icons[m.kind ?? "desktop"];
  const total = g.projects.reduce((n, p) => n + p.sessions.length, 0);
  return <Table.Tr className="fleet-row" data-selected={selected}>
    <Table.Td>
      <Group gap={4} wrap="nowrap">
        <ColorSwatch size={7} color={m.reachable ? "var(--mantine-color-success-filled)" : "transparent"} withShadow={false} aria-hidden style={{ border: m.reachable ? undefined : "1px solid var(--driga-muted)", flex: "none" }} />
        <Text fz="xs" ff="monospace" c="dimmed" truncate title={m.reachable ? "reachable" : `last seen ${relative(m.lastSeen)}`}>{m.reachable ? "reachable" : `seen ${relative(m.lastSeen)}`}</Text>
      </Group>
    </Table.Td>
    <Table.Td>
      <Group gap={4} wrap="nowrap">
        <ActionIcon size="sm" aria-label={`${open ? "Collapse" : "Expand"} ${m.name}`} aria-expanded={open} onClick={onToggle}>{open ? <CaretDown size={14} /> : <CaretRight size={14} />}</ActionIcon>
        <Glyph size={14} aria-hidden />
        <UnstyledButton className="fleet-row-title" onClick={() => onSelect({ kind: "machine", id: m.id })} aria-pressed={selected}><Text fz="sm" fw={600} ff="monospace">{m.name}</Text></UnstyledButton>
        {m.local && <Badge size="xs" variant="default">local</Badge>}
        <Text fz="xs" c="dimmed" ff="monospace">{plural(total, "session")}</Text>
      </Group>
    </Table.Td>
    <Table.Td />
    <Table.Td />
    <Table.Td ta="right"><Text fz="xs" ff="monospace" style={{ whiteSpace: "nowrap" }}>{bytes(m.memoryUsed)}<Text span fz="xs" ff="monospace" c="dimmed"> / {bytes(m.memoryTotal)}</Text></Text></Table.Td>
    <Table.Td />
    <Table.Td />
  </Table.Tr>;
}

function ProjectRow({ group: p, selected, onSelect }: { group: FleetProjectGroup; selected: boolean; onSelect: FleetActions["onSelect"] }) {
  return <Table.Tr className="fleet-row" data-selected={selected}>
    <Table.Td />
    <Table.Td>
      <Group gap={4} wrap="nowrap" pl={indent}>
        <FolderSimple size={14} aria-hidden />
        <UnstyledButton className="fleet-row-title" onClick={() => onSelect({ kind: "project", id: p.project.id })} aria-pressed={selected}><Text fz="sm" fw={500} ff="monospace">{p.project.name}</Text></UnstyledButton>
        <Text fz="xs" c="dimmed" ff="monospace">{plural(p.sessions.length, "session")}</Text>
      </Group>
    </Table.Td>
    <Table.Td />
    <Table.Td />
    <Table.Td ta="right"><Text fz="xs" ff="monospace" c="dimmed">{bytes(p.memory)}</Text></Table.Td>
    <Table.Td />
    <Table.Td />
  </Table.Tr>;
}

function SessionRow({ session: s, level, selected, onSelect, onOpen, onReply }: FleetActions & { session: FleetSession; level: number; selected: boolean }) {
  return <Table.Tr className="fleet-row" data-selected={selected}>
    <Table.Td><StatusBadge state={s.state} label={s.label} inferred={s.inferred} /></Table.Td>
    <Table.Td>
      <Group gap={4} wrap="nowrap" pl={indent * level} style={{ minWidth: 0 }}>
        {level > 2 && <Text fz="xs" c="dimmed" aria-hidden>↳</Text>}
        <UnstyledButton className="fleet-row-title" onClick={() => onSelect({ kind: "session", id: s.id })} aria-pressed={selected} style={{ minWidth: 0 }}>
          <Text fz="sm" truncate title={s.title}>{s.title}</Text>
        </UnstyledButton>
        <Text fz="xs" c="dimmed" ff="monospace" style={{ flex: "none" }}>#{s.id}</Text>
      </Group>
    </Table.Td>
    <Table.Td><AgentChip agent={s.agent} name size={16} /></Table.Td>
    <Table.Td>{s.branch && <Group gap={3} wrap="nowrap"><GitBranch size={12} aria-hidden style={{ flex: "none" }} /><Text fz="xs" ff="monospace" truncate title={s.branch}>{s.branch}</Text></Group>}</Table.Td>
    <Table.Td ta="right"><Text fz="xs" ff="monospace">{bytes(s.memory)}</Text></Table.Td>
    <Table.Td ta="right"><Text fz="xs" ff="monospace" c="dimmed" title={exact(s.lastActivity)}>{relative(s.lastActivity)}</Text></Table.Td>
    <Table.Td>
      <Group gap={2} wrap="nowrap" justify="flex-end" className="fleet-row-actions">
        {s.state === "waiting" && <Button size="compact-xs" variant="subtle" onClick={() => onReply(s)}>Reply</Button>}
        <Button size="compact-xs" variant="subtle" color="gray" onClick={() => onOpen(s)}>Open</Button>
      </Group>
    </Table.Td>
  </Table.Tr>;
}
