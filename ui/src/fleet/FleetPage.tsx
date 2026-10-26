import { Text } from "@mantine/core";
import { useMemo } from "react";
import type { Act } from "../panels/host";
import type { Fragment } from "../panels/types";
import type { Machine } from "../widgets/MachineCard";
import { FleetSankey } from "./FleetSankey";
import { fleetOf } from "./FleetSpec";
import type { FleetSession } from "./FleetTypes";

/** FleetPage is the Fleet as the app opens it: the Fleet spec's fragment,
 * which the app refreshes with the rest, mapped onto the view. Opening a
 * session jumps to its pane, as every session row does; replying opens the
 * pane too, since the answer is typed there. */
export function FleetPage({ fragment, inbox, strips, act }: {
  fragment: Fragment | null;
  /** The inbox, for why a session waits. */
  inbox?: Fragment | null;
  /** Each machine's strip of past polls, by machine id. */
  strips?: Record<string, Machine["strip"]>;
  act?: Act;
}) {
  const data = useMemo(() => fragment ? fleetOf(fragment, inbox) : null, [fragment, inbox]);
  if (!data) return <Text size="sm" c="dimmed">Loading…</Text>;
  const open = (s: FleetSession) => { if (act) void act({ kind: "jump", row: { session_id: Number(s.id), machine_id: s.machine } }); };
  return <FleetSankey {...data} strips={strips} onSelect={() => undefined} onOpen={open} onReply={open} />;
}
