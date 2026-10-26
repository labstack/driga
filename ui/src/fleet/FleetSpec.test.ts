import { describe, expect, it } from "vitest";
import { fleet, fleetEmpty } from "../fixtures/fleet";
import { fragments } from "../fixtures/fragments";
import { fleetOf, stateFor } from "./FleetSpec";

const GB = 1024 ** 3;

describe("fleetOf", () => {
  const data = fleetOf(fleet, fragments.inbox);
  it("reads machines with their reach, latest memory sample, leftovers and memory by role", () => {
    expect(data.machines.map(m => [m.id, m.name, m.local ?? false, m.reachable, m.kind])).toEqual([["studio-id", "studio", true, true, "desktop"], ["build-id", "build-01", false, true, "server"], ["laptop-id", "laptop-02", false, false, "desktop"]]);
    expect(data.machines[0].memoryUsed).toBe(25769803776);
    expect(data.machines[0].leftover).toEqual({ trees: 2, processes: 9, memory: 1288490188 + 859832320 });
    expect(data.machines[0].memoryByRole?.[0]).toEqual({ label: "claude · interactive", value: 2.6 * GB });
    expect(Date.parse(data.machines[2].lastSeen)).toBe(Date.parse("2026-03-14T14:46:00Z"));
    expect(data.machines[2].memoryUsed).toBe(11 * GB);
  });
  it("lists each project once, with its repo path", () => {
    expect(data.projects.map(p => p.id)).toEqual(["harbor", "atlas", "beacon", "sandbox"]);
    expect(data.projects[0].repo).toBe("/home/dev/src/harbor");
  });
  it("reads sessions with the flow's parent, start and last tool, and the inbox's reason for waiting", () => {
    const s = Object.fromEntries(data.sessions.map(s => [s.id, s]));
    expect(s["11"]).toMatchObject({ state: "waiting", agent: "claude", machine: "studio-id", branch: "feat/upload-limits", started: "2026-03-14T14:18:00Z", lastTool: "Bash(npm run migrate -- --env staging)", waiting: "permission · Bash · npm run migrate -- --env staging" });
    expect(s["13"].parent).toBe("12");
    expect(s["16"]).toMatchObject({ state: "stuck", inferred: true, evidence: "busy, no output for 34m, CPU idle" });
    expect(s["17"]).toMatchObject({ state: "stuck", label: "dead", inferred: true });
    expect(s["19"]).toMatchObject({ state: "idle", label: "unknown", inferred: true, memory: 136314880 });
    expect(s["14"].turns).toBeUndefined();
    expect(s["14"].files).toBeUndefined();
  });
  it("is empty when the service knows no machines", () => {
    expect(fleetOf(fleetEmpty)).toEqual({ machines: [], projects: [], sessions: [] });
  });
});

describe("stateFor", () => {
  it("keeps the service's states, reads stalled? as inferred stuck, dead as stuck, and the rest as inferred idle", () => {
    expect(stateFor("working")).toEqual({ state: "working", inferred: false });
    expect(stateFor("~working")).toEqual({ state: "working", inferred: true });
    expect(stateFor("stalled?")).toEqual({ state: "stuck", inferred: true });
    expect(stateFor("dead")).toEqual({ state: "stuck", label: "dead", inferred: true });
    expect(stateFor("unknown")).toEqual({ state: "idle", label: "unknown", inferred: true });
  });
});
