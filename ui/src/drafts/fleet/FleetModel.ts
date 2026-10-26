// Pure derivations both Fleet variants share: machines in a stable order,
// their projects and sessions grouped, subagents under their parent, file
// overlaps between sessions, and counts by state. Nothing here renders.
import { bytes } from "../../panels/format";
import { agents, states, type FleetData, type FleetMachine, type FleetProject, type FleetSession, type FleetState } from "./FleetTypes";

export type FleetProjectGroup = { project: FleetProject; sessions: FleetSession[]; memory: number; waiting: boolean };

export type FleetMachineGroup = { machine: FleetMachine; projects: FleetProjectGroup[]; memory: number; counts: Record<FleetState, number> };

export type FleetOverlap = { a: string; b: string; files: string[] };

/** sortedMachines puts the local machine first, then the rest by name, so
 * nothing reshuffles between refreshes. */
export function sortedMachines(machines: FleetMachine[]): FleetMachine[] {
  return [...machines].sort((a, b) => Number(!!b.local) - Number(!!a.local) || a.name.localeCompare(b.name));
}

/** byState orders sessions by what needs the person first, then by title. */
export function byState(a: FleetSession, b: FleetSession): number {
  return states[a.state].rank - states[b.state].rank || a.title.localeCompare(b.title);
}

export function emptyCounts(): Record<FleetState, number> {
  return { waiting: 0, stalled: 0, working: 0, idle: 0, finished: 0 };
}

export function counts(sessions: FleetSession[]): Record<FleetState, number> {
  const c = emptyCounts();
  for (const s of sessions) c[s.state]++;
  return c;
}

/** needsYou counts the sessions a person must act on. */
export function needsYou(sessions: FleetSession[]): number {
  return sessions.filter(s => s.state === "waiting" || s.state === "stalled").length;
}

/** groupFleet nests sessions under their project under their machine, in
 * stable order: machines local first then by name, projects by name,
 * sessions by state rank then title. A machine with no sessions still
 * appears, with no projects. */
export function groupFleet({ machines, projects, sessions }: FleetData): FleetMachineGroup[] {
  const byId = new Map(projects.map(p => [p.id, p]));
  return sortedMachines(machines).map(machine => {
    const own = sessions.filter(s => s.machine === machine.id);
    const groups = new Map<string, FleetSession[]>();
    for (const s of own) groups.set(s.project, [...(groups.get(s.project) ?? []), s]);
    const grouped: FleetProjectGroup[] = [...groups.entries()]
      .map(([id, list]) => ({ project: byId.get(id) ?? { id, name: id, repo: "" }, sessions: list.sort(byState), memory: list.reduce((n, s) => n + s.memory, 0), waiting: list.some(s => s.state === "waiting") }))
      .sort((a, b) => a.project.name.localeCompare(b.project.name));
    return { machine, projects: grouped, memory: own.reduce((n, s) => n + s.memory, 0), counts: counts(own) };
  });
}

/** childrenOf lists the subagents a session spawned, in state order. */
export function childrenOf(sessions: FleetSession[], id: string): FleetSession[] {
  return sessions.filter(s => s.parent === id).sort(byState);
}

/** roots keeps the sessions that no other session spawned, so a tree shows
 * each subagent once, under its parent. */
export function roots(sessions: FleetSession[]): FleetSession[] {
  const ids = new Set(sessions.map(s => s.id));
  return sessions.filter(s => !s.parent || !ids.has(s.parent));
}

/** overlaps finds pairs of sessions of one project editing the same file,
 * each pair once, with the files they share. */
export function overlaps(sessions: FleetSession[]): FleetOverlap[] {
  const out: FleetOverlap[] = [];
  for (let i = 0; i < sessions.length; i++) {
    for (let j = i + 1; j < sessions.length; j++) {
      const a = sessions[i], b = sessions[j];
      if (a.project !== b.project) continue;
      const files = a.files.filter(f => b.files.includes(f));
      if (files.length) out.push({ a: a.id, b: b.id, files });
    }
  }
  return out;
}

/** overlapsOf lists the files a session shares with any other session. */
export function overlapsOf(sessions: FleetSession[], id: string): { with: FleetSession; files: string[] }[] {
  const byId = new Map(sessions.map(s => [s.id, s]));
  return overlaps(sessions).filter(o => o.a === id || o.b === id).map(o => ({ with: byId.get(o.a === id ? o.b : o.a)!, files: o.files }));
}

/** turnSummary says what the last turns held, for the tracker strip and ring. */
export function turnSummary(session: FleetSession): { ok: number; waited: number; failed: number; total: number } {
  const last = session.turns.slice(-30);
  return { ok: last.filter(t => t === "ok").length, waited: last.filter(t => t === "waited").length, failed: last.filter(t => t === "failed").length, total: last.length };
}

export function basename(path: string): string {
  return path.slice(path.lastIndexOf("/") + 1);
}

export function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? "" : word.endsWith("s") ? "es" : "s"}`;
}

/** sessionCaption is the one mono line under a session's title:
 * agent, branch and memory. */
export function sessionCaption(s: FleetSession): string {
  return [agents[s.agent], s.branch, bytes(s.memory)].filter(Boolean).join(" · ");
}

export function machineOf(machines: FleetMachine[], id: string): FleetMachine | undefined {
  return machines.find(m => m.id === id);
}

export function projectOf(projects: FleetProject[], id: string): FleetProject | undefined {
  return projects.find(p => p.id === id);
}

/** machinesOfProject names the machines a project runs on, which answers
 * "the same repo on two machines" in words. */
export function machinesOfProject(data: FleetData, project: string): FleetMachine[] {
  const ids = new Set(data.sessions.filter(s => s.project === project).map(s => s.machine));
  return sortedMachines(data.machines.filter(m => ids.has(m.id)));
}
