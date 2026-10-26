import { Box, Stack, Text } from "@mantine/core";
import { useMemo } from "react";
import type { Act } from "../panels/host";
import { rows, type Fragment } from "../panels/types";
import { Heatmap } from "../widgets/Heatmap";
import { historyOf } from "../host/History";
import { machinesOf, stripsOf } from "../host/Header";
import { FleetSankey } from "./FleetSankey";
import { fleetOf } from "./FleetSpec";
import type { FleetSession } from "./FleetTypes";

/** FleetPage is the Fleet as the app opens it: the Fleet spec's fragment,
 * which the app refreshes with the rest, mapped onto the view. Opening a
 * session jumps to its pane, as every session row does; replying opens the
 * pane too, since the answer is typed there. */
export function FleetPage({ fragment, inbox, act }: {
  fragment: Fragment | null;
  /** The inbox, for why a session waits. */
  inbox?: Fragment | null;
  act?: Act;
}) {
  const data = useMemo(() => fragment ? fleetOf(fragment, inbox) : null, [fragment, inbox]);
  if (!data) return <Text size="sm" c="dimmed">Loading…</Text>;
  const open = (s: FleetSession) => { if (act) void act({ kind: "jump", row: { session_id: Number(s.id), machine_id: s.machine } }); };
  const kept = historyOf(fragment);
  const servedStrips = stripsOf(kept, machinesOf(data, kept.polls));
  const hours = rows(fragment?.results.find(r => r.id === "heatmap")?.frame);
  const projects = [...new Set(hours.map(r => String(r.series)))];
  const times = [...new Set(hours.map(r => String(r.ts)))].sort();
  const values = projects.map(p => times.map(t => Number(hours.find(r => r.series === p && r.ts === t)?.value ?? 0)));
  return <Stack gap="sm" style={{ flex: 1, minHeight: 0, overflowY: "auto" }}>
    <Box style={{ flex: 1, minHeight: 600 }}><FleetSankey {...data} strips={servedStrips} onSelect={() => undefined} onOpen={open} onReply={open} /></Box>
    {projects.length > 0 && <Box py="sm"><Text size="xs" fw={600} c="dimmed" mb={6}>Tool calls by project · last 24 hours</Text><Heatmap rows={projects} cols={times.map(t => new Date(t).toLocaleTimeString([], { hour: "2-digit", hour12: false }))} values={values} format={String} unit="tool calls" label="Tool calls by project, last 24 hours" /></Box>}
  </Stack>;
}
