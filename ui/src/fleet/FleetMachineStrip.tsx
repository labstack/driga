import { SimpleGrid } from "@mantine/core";
import { MachineCard, type Machine } from "../widgets/MachineCard";
import { machineCard, type FleetMachineGroup } from "./FleetModel";

/** FleetMachineStrip fills the available width with responsive machine
 * cards, wrapping when another 280px card cannot fit; selecting a
 * card narrows the view to that machine, selecting it again clears the
 * selection. A bounded scroll area leaves room for sessions on large fleets. */
export function FleetMachineStrip({ groups, strips, selected, onSelect }: { groups: FleetMachineGroup[]; strips?: Record<string, Machine["strip"]>; selected?: string | null; onSelect: (id: string | null) => void }) {
  return <SimpleGrid minColWidth={280} autoFlow="auto-fit" spacing="sm" component="ul" aria-label="Machines" m={0} p={0} style={{ flex: "none", listStyle: "none", maxHeight: "min(40vh, 18rem)", overflowY: "auto" }}>
    {groups.map(g => <li key={g.machine.id} style={{ minWidth: 0 }}>
      <MachineCard machine={machineCard(g, strips?.[g.machine.id])} selected={selected === g.machine.id} onSelect={m => onSelect(selected === m.id ? null : m.id ?? null)} />
    </li>)}
  </SimpleGrid>;
}
