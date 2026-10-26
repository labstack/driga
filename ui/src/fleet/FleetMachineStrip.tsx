import { Group, ScrollArea } from "@mantine/core";
import { MachineCard, type Machine } from "../widgets/MachineCard";
import { machineCard, type FleetMachineGroup } from "./FleetModel";

/** FleetMachineStrip is the row of machine cards above the view. It
 * scrolls sideways when the window is narrower than the fleet; selecting a
 * card narrows the view to that machine, selecting it again clears the
 * selection. */
export function FleetMachineStrip({ groups, strips, selected, onSelect }: { groups: FleetMachineGroup[]; strips?: Record<string, Machine["strip"]>; selected?: string | null; onSelect: (id: string | null) => void }) {
  return <ScrollArea type="auto" scrollbarSize={4} offsetScrollbars="present" style={{ flex: "none" }}>
    <Group gap="sm" align="flex-start" wrap="nowrap" component="ul" aria-label="Machines" m={0} p={0} style={{ listStyle: "none" }}>
      {groups.map(g => <li key={g.machine.id} style={{ flex: "none", width: 280 }}>
        <MachineCard machine={machineCard(g, strips?.[g.machine.id])} selected={selected === g.machine.id} onSelect={m => onSelect(selected === m.id ? null : m.id ?? null)} />
      </li>)}
    </Group>
  </ScrollArea>;
}
