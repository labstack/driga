import { describe, expect, it } from "vitest";
import { busy, sessions } from "./Fleet-fixtures";
import { FleetRelationships, counts, machineCard, needsYou, sortedMachines, turnCells, turnSummary, turnWords } from "./FleetModel";
import type { FleetSession } from "./FleetTypes";

describe("groupFleet", () => {
  const groups = new FleetRelationships(busy).groups;
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
    expect(groups[1].projects[1].sessions.map(s => s.state)).toEqual(["waiting", "stuck", "working"]);
  });
  it("sums memory per project and marks projects with a waiting session", () => {
    const atlas = groups[0].projects[0];
    expect(atlas.memory).toBe(sessions.filter(s => s.machine === "studio" && s.project === "atlas").reduce((n, s) => n + s.memory, 0));
    expect(atlas.waiting).toBe(true);
    expect(groups[0].projects[1].waiting).toBe(false);
  });
  it("keeps a machine without sessions", () => {
    const [only] = new FleetRelationships({ machines: busy.machines.slice(0, 1), projects: busy.projects, sessions: [] }).groups;
    expect(only.projects).toEqual([]);
    expect(only.counts.working).toBe(0);
  });
  it("shapes a machine for its card, with the live counts and leftovers", () => {
    const card = machineCard(groups[0]);
    expect(card).toMatchObject({ id: "studio", local: true, sessions: { working: 2, waiting: 1, stuck: 1, idle: 1 }, leftover: { trees: 3 } });
    expect(card.strip).toBeUndefined();
  });
});

describe("relations", () => {
  const model = new FleetRelationships(busy);
  it("owns machine identity, cross-project ancestry and project membership", () => {
    const base = sessions[0];
    const local: FleetSession = { ...base, id: "1", machine: "studio", project: "atlas", parent: undefined };
    const remote: FleetSession = { ...local, machine: "build-01" };
    const child: FleetSession = { ...local, id: "2", parent: "1", project: "harbor" };
    const grandchild: FleetSession = { ...child, id: "3", parent: "2" };
    const missing: FleetSession = { ...local, id: "4", parent: "remote-only" };
    const remoteParent: FleetSession = { ...remote, id: "remote-only" };
    const model = new FleetRelationships({ ...busy, sessions: [local, remote, child, grandchild, missing, remoteParent] });
    expect(model.session("1")).toBeUndefined();
    expect(model.session("1", "studio")).toBe(local);
    expect(model.parent(child)).toBe(local);
    expect(model.children(remote)).toEqual([]);
    expect(model.branch(local)).toEqual([{ session: local, depth: 0 }, { session: child, depth: 1 }, { session: grandchild, depth: 2 }]);
    expect(model.roots("studio", "harbor")).toEqual([]);
    expect(model.roots("studio", "atlas")).toContain(missing);
    expect(model.parent(missing)).toBeUndefined();
    expect(model.projectSessions("harbor")).toEqual([child, grandchild]);
    expect(model.projectMachines("harbor").map(m => m.id)).toEqual(["studio"]);
  });
  it("terminates ancestry cycles inside the module", () => {
    const a = { ...sessions[0], id: "a", parent: "b" };
    const b = { ...a, id: "b", parent: "a" };
    const model = new FleetRelationships({ ...busy, sessions: [a, b] });
    expect(model.branch(a)).toEqual([{ session: a, depth: 0 }, { session: b, depth: 1 }]);
  });
  it("nests subagents under their parent and keeps them out of the roots", () => {
    expect(model.children(sessions.find(s => s.id === "113")!).map(s => s.id)).toEqual(["118"]);
    expect(model.children(sessions.find(s => s.id === "095")!).map(s => s.id)).toEqual(["099"]);
    expect(model.groups.flatMap(g => g.projects.flatMap(p => model.roots(g.machine.id, p.project.id))).map(s => s.id)).not.toContain("118");
    expect(model.groups.flatMap(g => g.projects.flatMap(p => model.roots(g.machine.id, p.project.id)))).toHaveLength(12);
  });
  it("finds the one file two sessions of one project edit, across machines", () => {
    expect(sessions.flatMap(s => model.overlaps(s))).toEqual([]);
  });
  it("counts states and what needs the person", () => {
    expect(counts(sessions)).toEqual({ waiting: 3, stuck: 2, working: 5, idle: 2, finished: 2 });
    expect(needsYou(sessions)).toBe(5);
  });
  it("summarises the last 30 turns and names them", () => {
    const s = sessions.find(x => x.id === "104")!;
    expect(turnSummary(s)).toEqual({ ok: 26, waited: 0, failed: 4, total: 30 });
    expect(turnWords(s)).toBe("26 went through, 0 waited, 4 failed");
    expect(turnSummary(sessions.find(x => x.id === "118")!).total).toBe(7);
    expect(turnSummary({ ...s, turns: undefined }).total).toBe(0);
  });
  it("draws the turns as cells, padded before the first", () => {
    const cells = turnCells(sessions.find(x => x.id === "118")!);
    expect(cells).toHaveLength(30);
    expect(cells.slice(0, 23).every(c => c.state === null)).toBe(true);
    expect(cells[23].state).toBe("working");
  });
});
