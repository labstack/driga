import "./Fleet.css";
import { Card, Center, Group, Stack, Switch, Text } from "@mantine/core";
import { useUncontrolled } from "@mantine/hooks";
import { lazy, Suspense, useMemo, useState } from "react";
import { FleetEmpty } from "./FleetEmpty";
import { FleetHeader } from "./FleetHeader";
import { FleetInspector } from "./FleetInspector";
import { FleetMachineStrip } from "./FleetMachineStrip";
import { groupFleet } from "./FleetModel";
import type { FleetProps, FleetSelection } from "./FleetTypes";

// React Flow and ELK load only when the topology is opened.
const Canvas = lazy(() => import("./FleetTopologyCanvas").then(m => ({ default: m.FleetTopologyCanvas })));

/** FleetTopology is variant B of the Fleet view: the machine strip, then the
 * fleet as nested boxes on a canvas (machines hold projects hold session
 * cards, each a real component with its status ring, tracker strip and
 * actions), with the inspector beside it. Spawn edges join a parent to its
 * subagents; a red dashed edge joins sessions editing the same file.
 * Selecting a machine card narrows the canvas to that machine. */
export function FleetTopology({ machines, projects, sessions, selected, defaultSelected, onSelect, onOpen, onReply }: FleetProps) {
  const data = useMemo(() => ({ machines, projects, sessions }), [machines, projects, sessions]);
  const [selection, select] = useUncontrolled<FleetSelection | null>({ value: selected, defaultValue: defaultSelected, finalValue: null, onChange: onSelect });
  const [minimap, setMinimap] = useState(false);
  const groups = useMemo(() => groupFleet(data), [data]);
  const machineId = selection?.kind === "machine" ? selection.id : null;
  const shown = useMemo(() => machineId ? { machines: data.machines.filter(m => m.id === machineId), projects: data.projects, sessions: data.sessions.filter(s => s.machine === machineId) } : data, [data, machineId]);

  if (!machines.length) return <Stack gap="sm" h="100%"><FleetHeader {...data} /><FleetEmpty /></Stack>;
  return <Stack gap="sm" h="100%" style={{ minHeight: 0 }}>
    <FleetHeader {...data}>
      <Switch size="xs" label="Minimap" checked={minimap} onChange={e => setMinimap(e.currentTarget.checked)} />
    </FleetHeader>
    <FleetMachineStrip groups={groups} selected={machineId} onSelect={id => select(id ? { kind: "machine", id } : null)} />
    <Group align="stretch" wrap="nowrap" gap="sm" style={{ flex: 1, minHeight: 0 }}>
      <Card padding={0} style={{ flex: 1, minWidth: 0, display: "flex", overflow: "hidden" }}>
        <Suspense fallback={<Center style={{ flex: 1 }}><Text fz="sm" c="dimmed">Loading the fleet map…</Text></Center>}>
          <Canvas data={shown} selection={selection} minimap={minimap} onSelect={select} onOpen={onOpen} onReply={onReply} />
        </Suspense>
      </Card>
      <FleetInspector data={data} selection={selection} onSelect={select} onOpen={onOpen} onReply={onReply} />
    </Group>
  </Stack>;
}
