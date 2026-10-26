import "./Fleet.css";
import { Badge, Button, Center, Group, Paper, RingProgress, Stack, Text } from "@mantine/core";
import { CheckCircle, HandPalm, Pause, Pulse, WarningOctagon, type Icon } from "@phosphor-icons/react";
import { Handle, Position, useNodeId, type Node, type NodeProps } from "@xyflow/react";
import { bytes } from "../../panels/format";
import { basename, turnSummary } from "./FleetModel";
import { sessionSize } from "./FleetTopologyLayout";
import { FleetTurnStrip } from "./FleetTurnStrip";
import { agents, states, toneVar, type FleetSession, type FleetState } from "./FleetTypes";

export type SessionNodeData = {
  session: FleetSession;
  subagents: number;
  /** Files other sessions of the project are also editing. */
  shared: string[];
  dimmed: boolean;
  onOpen: (session: FleetSession) => void;
  onReply: (session: FleetSession) => void;
};

export type SessionNode = Node<SessionNodeData, "session">;

const icons: Record<FleetState, Icon> = { working: Pulse, waiting: HandPalm, stalled: WarningOctagon, idle: Pause, finished: CheckCircle };

/** FleetSessionNode is one session on the topology canvas: a status ring
 * whose colour is the state and whose arcs are the last 30 turns (waits in
 * yellow, failures in red), the title and agent, id, branch and memory, the
 * 30-turn tracker strip, the last tool, and Reply when it waits for the
 * person, Open always. Outside a canvas it renders without ports. */
export function FleetSessionNode({ data, selected }: NodeProps<SessionNode>) {
  const { session: s } = data;
  const tone = states[s.state].tone;
  const { ok, waited, failed, total } = turnSummary(s);
  const sections = total
    ? [{ value: (ok / total) * 100, color: toneVar(tone) }, { value: (waited / total) * 100, color: toneVar("warning") }, { value: (failed / total) * 100, color: toneVar("danger") }].filter(x => x.value > 0)
    : [];
  const Glyph = icons[s.state];
  const inFlow = useNodeId() !== null;
  const caption = [s.branch, bytes(s.memory)].filter(Boolean).join(" · ");
  const stop = (e: React.MouseEvent) => e.stopPropagation();
  return <Paper className="fleet-node" data-dimmed={data.dimmed} data-selected={selected} w={sessionSize.width} h={sessionSize.height} p={10} radius="md" withBorder>
    <Stack gap={6} h="100%" justify="space-between">
      <Group gap={8} wrap="nowrap" align="flex-start">
        <RingProgress size={32} thickness={3} sections={sections} rootColor="var(--driga-border)" role="img" aria-label={`${states[s.state].label}${s.inferred ? ", inferred" : ""}: ${ok} of ${total} turns ok`} style={{ flex: "none" }} label={<Center><Glyph size={14} color={toneVar(tone)} aria-hidden /></Center>} />
        <Stack gap={1} style={{ flex: 1, minWidth: 0 }}>
          <Text fz="sm" fw={600} lh={1.25} truncate title={`${s.title} · #${s.id}`}>{s.title}</Text>
          <Group gap={6} wrap="nowrap" justify="space-between">
            <Group gap={4} wrap="nowrap" style={{ minWidth: 0 }}>
              <Text fz="xs" c="dimmed" ff="monospace" truncate title={`#${s.id} · ${caption}`} style={{ minWidth: 0 }}>{caption}</Text>
              {data.subagents > 0 && <Text fz="xs" c="dimmed" ff="monospace" style={{ flex: "none" }} title={`${data.subagents} subagents`}>↳{data.subagents}</Text>}
              {data.shared.length > 0 && <Text fz="xs" c="danger" fw={700} ff="monospace" style={{ flex: "none" }} title={`Also edited elsewhere: ${data.shared.map(basename).join(", ")}`}>!</Text>}
            </Group>
            <Badge variant="default" size="xs" style={{ flex: "none" }}>{agents[s.agent]}</Badge>
          </Group>
        </Stack>
      </Group>
      <FleetTurnStrip session={s} />
      <Group gap={4} wrap="nowrap" justify="space-between">
        <Text fz="xs" c="dimmed" ff="monospace" truncate title={s.lastTool ?? ""} style={{ minWidth: 0 }}>{s.lastTool ?? "no tool yet"}</Text>
        <Group gap={0} wrap="nowrap" className="nodrag nopan" style={{ flex: "none" }}>
          {s.state === "waiting" && <Button size="compact-xs" variant="subtle" onClick={e => { stop(e); data.onReply(s); }}>Reply</Button>}
          <Button size="compact-xs" variant="subtle" color="gray" onClick={e => { stop(e); data.onOpen(s); }}>Open</Button>
        </Group>
      </Group>
    </Stack>
    {inFlow && <>
      <Handle type="target" position={Position.Left} isConnectable={false} className="fleet-handle" />
      <Handle type="source" position={Position.Right} isConnectable={false} className="fleet-handle" />
    </>}
  </Paper>;
}
