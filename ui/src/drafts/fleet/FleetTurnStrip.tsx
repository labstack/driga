import "./Fleet.css";
import { turnSummary } from "./FleetModel";
import type { FleetSession } from "./FleetTypes";

/** FleetTurnStrip is the 30-cell tracker of a session's last turns, as
 * Tremor's Tracker and Grafana's status history draw uptime: quiet cells
 * for turns that went through, yellow where the agent waited on the person,
 * red where a turn failed. Cells before the session's first turn are empty. */
export function FleetTurnStrip({ session }: { session: FleetSession }) {
  const { ok, waited, failed, total } = turnSummary(session);
  const last = session.turns.slice(-30);
  const pad = 30 - last.length;
  const text = `Last ${total} turns: ${ok} ok, ${waited} waited, ${failed} failed`;
  return <div role="img" aria-label={text} title={text} className="fleet-turns">
    {Array.from({ length: 30 }, (_, i) => <span key={i} data-turn={i < pad ? "none" : last[i - pad]} />)}
  </div>;
}
