import { describe, expect, it } from "vitest";
import { busy, sessions } from "./Fleet-fixtures";
import { childrenOf, counts, groupFleet, needsYou, overlaps, roots, sortedMachines, turnSummary } from "./FleetModel";

describe("groupFleet", () => {
  const groups = groupFleet(busy);
  it("puts the local machine first and the rest by name", () => {
    expect(groups.map(g => g.machine.id)).toEqual(["studio", "build-01", "laptop-02"]);
    expect(sortedMachines([...busy.machines].reverse()).map(m => m.id)).toEqual(["studio", "build-01", "laptop-02"]);
  });
  it("groups each machine's sessions by project, projects by name", () => {
    expect(groups[0].projects.map(p => p.project.id)).toEqual(["atlas", "harbor", "sandbox"]);
    expect(groups[1].projects.map(p => p.project.id)).toEqual(["atlas", "beacon"]);
  });
  it("orders sessions by what needs the person first, then title", () => {
    expect(groups[0].projects[0].sessions.map(s => s.id)).toEqual(["113", "112", "118"]);
    expect(groups[1].projects[1].sessions.map(s => s.state)).toEqual(["waiting", "stalled", "working"]);
  });
  it("sums memory per project and marks projects with a waiting session", () => {
    const atlas = groups[0].projects[0];
    expect(atlas.memory).toBe(sessions.filter(s => s.machine === "studio" && s.project === "atlas").reduce((n, s) => n + s.memory, 0));
    expect(atlas.waiting).toBe(true);
    expect(groups[0].projects[1].waiting).toBe(false);
  });
  it("keeps a machine without sessions", () => {
    const [only] = groupFleet({ machines: busy.machines.slice(0, 1), projects: busy.projects, sessions: [] });
    expect(only.projects).toEqual([]);
    expect(only.counts.working).toBe(0);
  });
});

describe("relations", () => {
  it("nests subagents under their parent and keeps them out of the roots", () => {
    expect(childrenOf(sessions, "113").map(s => s.id)).toEqual(["118"]);
    expect(childrenOf(sessions, "095").map(s => s.id)).toEqual(["099"]);
    expect(roots(sessions).map(s => s.id)).not.toContain("118");
    expect(roots(sessions)).toHaveLength(12);
  });
  it("finds the one file two sessions of one project edit, across machines", () => {
    expect(overlaps(sessions)).toEqual([{ a: "112", b: "090", files: ["internal/lifeline/clock.go"] }]);
  });
  it("counts states and what needs the person", () => {
    expect(counts(sessions)).toEqual({ waiting: 3, stalled: 2, working: 5, idle: 2, finished: 2 });
    expect(needsYou(sessions)).toBe(5);
  });
  it("summarises the last 30 turns", () => {
    const s = sessions.find(x => x.id === "104")!;
    expect(turnSummary(s)).toEqual({ ok: 26, waited: 0, failed: 4, total: 30 });
    expect(turnSummary(sessions.find(x => x.id === "118")!).total).toBe(7);
  });
});
