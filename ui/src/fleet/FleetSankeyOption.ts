// The Fleet as a sankey: machines, projects and sessions in three columns,
// link width = memory, node colour = state. This module builds the nodes
// and links; the app's chart module draws them.
import { alpha } from "@mantine/core";
import { escape, sankey, type Option, type SankeyLink, type SankeyNode, type Theme } from "../panels/charts";
import { bytes, plural, relative } from "../panels/format";
import { fonts, typeScale } from "../theme/tokens";
import { agentLabel } from "../widgets/types";
import { childrenOf, groupFleet, machinesOfProject, overlapsOf, sortedMachines } from "./FleetModel";
import { states, type FleetData, type FleetSelection } from "./FleetTypes";

/** sankeyHeight gives every session row room, and never less than a panel. */
export function sankeyHeight(sessions: number): number {
  return Math.max(360, 26 * sessions + 48);
}

/** selectionOf reads what a click on the chart meant: a node selects it,
 * a link selects its target. */
export function selectionOf(params: { dataType?: string; name?: string; data?: unknown }): FleetSelection | null {
  const name = params.dataType === "edge" ? (params.data as { target?: string } | undefined)?.target : params.name;
  if (!name) return null;
  const [prefix, id] = [name.slice(0, 2), name.slice(2)];
  if (prefix === "m:" || prefix === "l:") return { kind: "machine", id };
  if (prefix === "p:") return { kind: "project", id };
  if (prefix === "s:") return { kind: "session", id };
  return null;
}

/** rich escapes a label for ECharts rich text, whose braces mark styles. */
function rich(text: string): string {
  return text.replace(/[{}|]/g, " ");
}

/** fleetSankey draws the fleet: machines (grey, dashed when unreachable)
 * feed projects (the accent) feed sessions (their state's colour), with
 * leftovers hanging off each machine as a dashed pseudo-project. A waiting
 * session's path is yellow from the machine column on. Sessions keep their
 * project's order, then their machine's, then state rank, so each
 * project's fan stays together and nothing crosses twice. */
export function fleetSankey(data: FleetData, t: Theme): Option {
  const c = { ...t.states, brand: t.brand, machine: t.line, leftover: t.muted };
  const groups = groupFleet(data);
  const machines = sortedMachines(data.machines);
  const projects = [...new Set(data.sessions.map(s => s.project))].map(id => data.projects.find(p => p.id === id) ?? { id, name: id, repo: "" }).sort((a, b) => a.name.localeCompare(b.name));
  const total = data.sessions.reduce((n, s) => n + s.memory, 0) + machines.reduce((n, m) => n + (m.leftover?.memory ?? 0), 0);
  const floor = Math.max(1, total / 200);
  const nodes: SankeyNode[] = [];
  const links: SankeyLink[] = [];
  const quiet = { color: "gradient", opacity: 0.22 };
  const needs = { color: c.waiting, opacity: 0.4 };

  for (const g of groups) {
    const m = g.machine;
    const sessions = g.projects.reduce((n, p) => n + p.sessions.length, 0);
    nodes.push({
      name: `m:${m.id}`, depth: 0, text: rich(m.name),
      tip: `<b>${escape(m.name)}</b>${m.local ? " · local" : ""}<br>${m.reachable ? "reachable" : `unreachable · last seen ${escape(relative(m.lastSeen))}`}<br>${escape(bytes(m.memoryUsed))} of ${escape(bytes(m.memoryTotal))} used · ${plural(sessions, "session")}`,
      itemStyle: { color: c.machine, borderColor: m.reachable ? c.machine : t.muted, borderWidth: 1, borderType: m.reachable ? "solid" : "dashed" },
      label: { position: "left", fontFamily: fonts.sans, fontSize: typeScale.small + 1, fontWeight: 600, color: t.text },
    });
  }
  for (const p of projects) {
    const on = machinesOfProject(data, p.id);
    const sessions = data.sessions.filter(s => s.project === p.id);
    nodes.push({
      name: `p:${p.id}`, depth: 1, text: rich(p.name),
      tip: `<b>${escape(p.name)}</b><br>${escape(p.repo)}<br>on ${escape(on.map(m => m.name).join(", "))} · ${plural(sessions.length, "session")} · ${escape(bytes(sessions.reduce((n, s) => n + s.memory, 0)))}`,
      itemStyle: { color: alpha(c.brand, 0.55) },
      label: { fontFamily: fonts.sans, fontSize: typeScale.small, color: t.text },
    });
  }
  for (const g of groups) {
    const m = g.machine;
    if (!m.leftover) continue;
    nodes.push({
      name: `l:${m.id}`, depth: 1, text: rich(`leftover · ${plural(m.leftover.processes, "proc")} · ${bytes(m.leftover.memory)}`),
      tip: `<b>Leftover on ${escape(m.name)}</b><br>${plural(m.leftover.processes, "process")} no session owns · ${escape(bytes(m.leftover.memory))} reclaimable`,
      itemStyle: { color: "transparent", borderColor: c.leftover, borderWidth: 1, borderType: "dashed" },
      label: { fontFamily: fonts.mono, fontSize: typeScale.small, color: t.muted },
    });
    links.push({ source: `m:${m.id}`, target: `l:${m.id}`, value: Math.max(m.leftover.memory, floor), tip: `${escape(m.name)} → leftover: ${escape(bytes(m.leftover.memory))}`, lineStyle: { color: c.leftover, opacity: 0.14 } });
  }
  for (const p of projects) {
    for (const g of groups) {
      const pg = g.projects.find(x => x.project.id === p.id);
      if (!pg) continue;
      links.push({ source: `m:${g.machine.id}`, target: `p:${p.id}`, value: Math.max(pg.memory, floor), tip: `${escape(g.machine.name)} → ${escape(p.name)}: ${escape(bytes(pg.memory))} in ${plural(pg.sessions.length, "session")}`, lineStyle: pg.waiting ? needs : quiet });
      for (const s of pg.sessions) {
        const kids = childrenOf(data.sessions, s.id).length;
        const shared = overlapsOf(data.sessions, s.id);
        const marks = `${kids ? ` {dim|↳${kids}}` : ""}${shared.length ? " {bang|!}" : ""}`;
        nodes.push({
          name: `s:${s.id}`, depth: 2,
          text: `${rich(s.title)}${marks}{dim| · ${rich(s.agent)} · ${bytes(s.memory)}}`,
          tip: [
            `<b>${escape(s.title)}</b>`,
            `${escape(s.label ?? states[s.state].label)}${s.inferred ? " (inferred)" : ""} · ${escape(agentLabel(s.agent))}${s.branch ? ` · ${escape(s.branch)}` : ""}`,
            `${escape(p.name)} on ${escape(g.machine.name)} · ${escape(bytes(s.memory))}`,
            s.lastTool ? `${escape(s.lastTool)} · ${escape(relative(s.lastActivity))}` : "",
            s.waiting ? escape(s.waiting) : "",
            kids ? plural(kids, "subagent") : "",
            ...shared.map(o => `also editing ${escape(o.files.map(f => f.slice(f.lastIndexOf("/") + 1)).join(", "))} with ${escape(o.with.title)}`),
          ].filter(Boolean).join("<br>"),
          itemStyle: { color: c[s.state] },
        });
        links.push({ source: `p:${p.id}`, target: `s:${s.id}`, value: Math.max(s.memory, floor), tip: `${escape(p.name)} → ${escape(s.title)}: ${escape(bytes(s.memory))}`, lineStyle: s.state === "waiting" ? needs : quiet });
      }
    }
  }

  const need = data.sessions.filter(s => s.state === "waiting" || s.state === "stuck").length;
  return sankey({
    nodes, links,
    description: `Fleet flow: ${plural(machines.length, "machine")}, ${plural(projects.length, "project")}, ${plural(data.sessions.length, "session")}, ${need} needing you. Link width is memory, node colour is state.`,
    rich: { dim: { color: t.muted, fontFamily: fonts.mono, fontSize: typeScale.small }, bang: { color: c.stuck, fontWeight: 700, fontFamily: fonts.mono, fontSize: typeScale.small } },
  }, t);
}
