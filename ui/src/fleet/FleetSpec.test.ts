import { describe, expect, it } from "vitest";
import { fleet, fleetEmpty } from "../fixtures/fleet";
import { fragments } from "../fixtures/fragments";
import { fleetOf, stateFor } from "./FleetSpec";
import type { Cell, Fragment, Frame } from "../panels/types";

const frame = (columns: string[], values: Cell[][]): Frame => ({ columns: columns.map(name => ({ name, type: "string" })), rows: values });
const withFrames = (frames: Record<string, Frame>): Fragment => ({ ...fleetEmpty, results: Object.entries(frames).map(([id, frame]) => ({ id, status: "ok", frame })) });

const GB = 1024 ** 3;

describe("fleetOf", () => {
  const data = fleetOf(fleet, fragments.inbox);
  it("reads machines with their reach, latest memory sample, leftovers and memory by role", () => {
    expect(data.machines.map(m => [m.id, m.name, m.local ?? false, m.reachable, m.kind])).toEqual([["studio-id", "studio", true, true, "desktop"], ["build-id", "build-01", false, true, "server"], ["laptop-id", "laptop-02", false, false, "desktop"]]);
    expect(data.machines[0].memoryUsed).toBe(25769803776);
    expect(data.machines[0]).toMatchObject({ cpuPercent: 42, diskFree: 120 * GB, diskTotal: 500 * GB });
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
    expect(s["14"].turns).toEqual([]);
    expect(s["14"].files).toEqual([]);
    expect(s["11"].turns).toEqual(["ok", "waited", "failed"]);
    expect(s["11"].files).toEqual(["src/upload.ts"]);
  });
  it("preserves unknown disk measurements rather than reporting a full disk", () => {
    const data = fleetOf(withFrames({ machines: frame(["machine_id", "disk_free_bytes", "disk_total_bytes"], [["missing", null, 500], ["full", 0, 500], ["unknown", null, null]]) }));
    expect(data.machines[0]).toMatchObject({ diskFree: undefined, diskTotal: 500 });
    expect(data.machines[1]).toMatchObject({ diskFree: 0, diskTotal: 500 });
    expect(data.machines[2]).toMatchObject({ diskFree: undefined, diskTotal: undefined });
  });
  it("joins flow and inbox rows by machine and session, preserving string identifiers", () => {
    const fragment = withFrames({
      sessions: frame(["id", "machine_id", "label"], [["child", "a", "waiting"], ["child", "b", "waiting"]]),
      flow: frame(["id", "machine_id", "parent_id", "last_tool", "started_at"], [["child", "a", "parent-a", "Edit(a)", "2026-03-14T14:00:00Z"], ["child", "b", "parent-b", "Edit(b)", "2026-03-14T14:30:00Z"]]),
    });
    const inbox = withFrames({ inbox: frame(["session_id", "machine_id", "kind", "reason"], [["child", "a", "waiting", "permission-a"], ["child", "b", "waiting", "permission-b"]]) });
    const data = fleetOf(fragment, inbox);
    expect(data.sessions[0]).toMatchObject({ machine: "a", parent: "parent-a", lastTool: "Edit(a)", started: "2026-03-14T14:00:00Z", waiting: "permission-a" });
    expect(data.sessions[1]).toMatchObject({ machine: "b", parent: "parent-b", lastTool: "Edit(b)", started: "2026-03-14T14:30:00Z", waiting: "permission-b" });
  });
  it.each(["not json", '{"path":"file.go"}', "null", "42", '["file.go", null, 1]'])("validates edited paths from %s", edited => {
    const data = fleetOf(withFrames({ sessions: frame(["id", "edited"], [[1, edited]]) }));
    expect(data.sessions[0].files).toEqual(edited.startsWith("[") ? ["file.go"] : []);
  });
  it("reads last_seen as milliseconds before the RFC3339 observation", () => {
    const data = fleetOf(withFrames({ machines: frame(["machine_id", "last_seen"], [["a", 4_000]]) }));
    expect(Date.parse(data.machines[0].lastSeen)).toBe(Date.parse(fleetEmpty.observed_at) - 4_000);
  });
  it("is empty when the service knows no machines", () => {
    expect(fleetOf(fleetEmpty)).toEqual({ machines: [], projects: [], sessions: [] });
  });
});

describe("stateFor", () => {
  it("keeps the service's states, reads stuck as inferred stuck, dead as stuck, and the rest as inferred idle", () => {
    expect(stateFor("working")).toEqual({ state: "working", inferred: false });
    expect(stateFor("~working")).toEqual({ state: "working", inferred: true });
    expect(stateFor("stuck")).toEqual({ state: "stuck", inferred: false });
    expect(stateFor("~stuck")).toEqual({ state: "stuck", inferred: true });
    expect(stateFor("dead")).toEqual({ state: "stuck", label: "dead", inferred: true });
    expect(stateFor("unknown")).toEqual({ state: "idle", label: "unknown", inferred: true });
  });
});
