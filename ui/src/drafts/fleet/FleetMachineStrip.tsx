import { Group, ScrollArea } from "@mantine/core";
import { FleetMachineCard } from "./FleetMachineCard";
import type { FleetMachineGroup } from "./FleetModel";

/** FleetMachineStrip is the row of machine cards above either view. It
 * scrolls sideways when the window is narrower than the fleet; selecting a
 * card again clears the selection. */
export function FleetMachineStrip({ groups, selected, onSelect }: { groups: FleetMachineGroup[]; selected?: string | null; onSelect: (id: string | null) => void }) {
  return <ScrollArea type="auto" scrollbarSize={4} offsetScrollbars="present" style={{ flex: "none" }}>
    <Group gap="sm" wrap="nowrap" component="ul" aria-label="Machines" m={0} p={0} style={{ listStyle: "none" }}>
      {groups.map(g => <li key={g.machine.id} style={{ flex: "none" }}>
        <FleetMachineCard group={g} selected={selected === g.machine.id} onSelect={id => onSelect(selected === id ? null : id)} />
      </li>)}
    </Group>
  </ScrollArea>;
}
