import "./Fleet.css";
import { Box, Card, Flex, SegmentedControl, Stack, Text } from "@mantine/core";
import { useUncontrolled } from "@mantine/hooks";
import { useCallback, useMemo } from "react";
import { Chart, type ChartEvent } from "../panels/Chart";
import type { Theme } from "../panels/charts";
import type { Machine } from "../widgets/MachineCard";
import { FleetEmpty } from "./FleetEmpty";
import { FleetHeader } from "./FleetHeader";
import { FleetInspector } from "./FleetInspector";
import { FleetMachineStrip } from "./FleetMachineStrip";
import { groupFleet } from "./FleetModel";
import { fleetSankey, sankeyHeight, selectionOf } from "./FleetSankeyOption";
import { FleetTree } from "./FleetTree";
import type { FleetProps, FleetSelection } from "./FleetTypes";

export type FleetMode = "flow" | "tree";

const modes = [{ value: "flow", label: "Flow" }, { value: "tree", label: "Tree" }];

/** FleetSankey is the Fleet view: the machine strip, then the fleet as a
 * sankey from machines through projects to sessions, where link width is
 * memory and node colour is state, with the inspector beside it, or below
 * it on a laptop. Node order is fixed (machines local first then by name,
 * projects by name, sessions by project then state), so nothing reshuffles
 * between refreshes. Hovering a node lights its whole path; a waiting
 * session's path is yellow from the machine column on. Tree mode is the
 * same fleet as an accessible table, for the keyboard and screen readers.
 * Selecting a machine card narrows both modes to that machine. */
export function FleetSankey({ machines, projects, sessions, strips, selected, defaultSelected, onSelect, onOpen, onReply, mode, defaultMode, onModeChange }: FleetProps & {
  /** Each machine's strip of past polls, by machine id, when the app has kept any. */
  strips?: Record<string, Machine["strip"]>;
  mode?: FleetMode; defaultMode?: FleetMode; onModeChange?: (mode: FleetMode) => void;
}) {
  const data = useMemo(() => ({ machines, projects, sessions }), [machines, projects, sessions]);
  const [selection, select] = useUncontrolled<FleetSelection | null>({ value: selected, defaultValue: defaultSelected, finalValue: null, onChange: onSelect });
  const [view, setView] = useUncontrolled<FleetMode>({ value: mode, defaultValue: defaultMode, finalValue: "flow", onChange: onModeChange });
  const groups = useMemo(() => groupFleet(data), [data]);
  const machineId = selection?.kind === "machine" ? selection.id : null;
  const shown = useMemo(() => machineId ? { machines: data.machines.filter(m => m.id === machineId), projects: data.projects, sessions: data.sessions.filter(s => s.machine === machineId) } : data, [data, machineId]);
  const option = useCallback((t: Theme) => fleetSankey(shown, t), [shown]);
  const onEvent = useCallback((_: string, p: ChartEvent) => { const s = selectionOf(p); if (s) select(s); }, [select]);

  if (!machines.length) return <Stack gap="sm" h="100%"><FleetHeader {...data} /><FleetEmpty /></Stack>;
  return <Stack gap="sm" h="100%" style={{ minHeight: 0 }}>
    <FleetHeader {...data}>
      <SegmentedControl size="xs" value={view} onChange={v => setView(v as FleetMode)} data={modes} aria-label="View" />
    </FleetHeader>
    <FleetMachineStrip groups={groups} strips={strips} selected={machineId} onSelect={id => select(id ? { kind: "machine", id } : null)} />
    {/* Below the lg breakpoint the inspector moves under the chart and the body scrolls as a whole;
        above it the chart card scrolls on its own. Both are reachable by keyboard. */}
    <Flex className="fleet-body" direction={{ base: "column", lg: "row" }} align="stretch" wrap="nowrap" gap="sm" role="region" aria-label="Fleet" tabIndex={0}>
      <Card padding="sm" className="fleet-chart" role="region" aria-label="Fleet flow" tabIndex={0}>
        {view === "flow"
          ? <Stack gap={4} style={{ flex: 1, minHeight: 0 }}>
            {shown.sessions.length
              ? <Box style={{ flex: 1, minHeight: sankeyHeight(shown.sessions.length), display: "flex", flexDirection: "column" }}><Chart option={option} label="Fleet flow from machines through projects to sessions" height="100%" onEvent={onEvent} /></Box>
              : <Text fz="sm" c="dimmed" py="xl" ta="center">No sessions on this machine right now.</Text>}
            <Text ff="monospace" fz="xs" c="dimmed" style={{ flex: "none" }}>width = memory · colour = state</Text>
          </Stack>
          : <FleetTree data={shown} selected={selection} onSelect={select} onOpen={onOpen} onReply={onReply} />}
      </Card>
      <FleetInspector data={data} selection={selection} onSelect={select} onOpen={onOpen} onReply={onReply} />
    </Flex>
  </Stack>;
}
