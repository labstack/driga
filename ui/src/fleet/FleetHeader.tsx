import { Group, Text, Title } from "@mantine/core";
import type { ReactNode } from "react";
import { plural } from "../panels/format";
import { needsYou } from "./FleetModel";
import type { FleetData } from "./FleetTypes";

/** FleetHeader is the page's header: the title, the fleet's counts in
 * mono, how many sessions need the person, and the view's controls on
 * the right. */
export function FleetHeader({ machines, sessions, children }: FleetData & { children?: ReactNode }) {
  const need = needsYou(sessions);
  const projects = new Set(sessions.map(s => s.project)).size;
  return <Group justify="space-between" wrap="nowrap" gap="sm" style={{ flex: "none" }}>
    <Group gap="sm" wrap="nowrap" align="baseline" style={{ minWidth: 0 }}>
      <Title order={1} fz={28} fw={650} lh={1.2} style={{ letterSpacing: "-0.02em" }}>Fleet</Title>
      <Text ff="monospace" fz="xs" c="dimmed" style={{ whiteSpace: "nowrap" }}>{plural(machines.length, "machine")} · {plural(projects, "project")} · {plural(sessions.length, "session")}</Text>
      {need > 0 && <Text ff="monospace" fz="xs" fw={600} c="warning" style={{ whiteSpace: "nowrap" }}>· {need} need you</Text>}
    </Group>
    <Group gap="xs" wrap="nowrap">{children}</Group>
  </Group>;
}
