// The topology's layout: ELK's layered algorithm places machine boxes in a
// row, each machine's project boxes inside it, and each project's sessions
// inside that, with spawned subagents to the right of their parent. The
// result is deterministic for the same fleet, so nothing drifts between
// refreshes. Layout runs per level (ELK's default hierarchy handling) rather
// than as one included hierarchy: one included layered pass forces a single
// direction on every level, which measured 903×1468 or 2472×607 for this
// fleet against 1364×584 here. React Flow draws the few cross-box edges
// itself, so ELK need not route them.
import ELK, { type ElkNode } from "elkjs/lib/elk.bundled.js";
import { groupFleet } from "./FleetModel";
import type { FleetData } from "./FleetTypes";

export const sessionSize = { width: 272, height: 100 } as const;

const emptyMachine = { width: 260, height: 72 } as const;

export type Placed = { id: string; x: number; y: number; width: number; height: number };

export type FleetLayout = { machines: Placed[]; projects: (Placed & { parent: string })[]; sessions: (Placed & { parent: string })[] };

export const machineNode = (id: string) => `m:${id}`;
export const projectNode = (machine: string, project: string) => `p:${machine}/${project}`;
export const sessionNode = (id: string) => `s:${id}`;

const layered = { "elk.algorithm": "layered", "elk.direction": "RIGHT", "elk.spacing.nodeNode": "12", "elk.layered.spacing.nodeNodeBetweenLayers": "28" };

function graphOf(data: FleetData): ElkNode {
  return {
    id: "fleet",
    layoutOptions: { "elk.algorithm": "rectpacking", "elk.aspectRatio": "2.4", "elk.spacing.nodeNode": "24", "elk.padding": "[top=0,left=0,bottom=0,right=0]" },
    children: groupFleet(data).map(g => ({
      id: machineNode(g.machine.id),
      layoutOptions: { ...layered, "elk.padding": "[top=44,left=12,bottom=12,right=12]" },
      ...(g.projects.length ? {} : emptyMachine),
      children: g.projects.map(p => ({
        id: projectNode(g.machine.id, p.project.id),
        layoutOptions: { ...layered, "elk.padding": "[top=36,left=12,bottom=12,right=12]" },
        children: p.sessions.map(s => ({ id: sessionNode(s.id), ...sessionSize })),
        edges: p.sessions
          .filter(s => s.parent && p.sessions.some(x => x.id === s.parent))
          .map(s => ({ id: `spawn:${s.id}`, sources: [sessionNode(s.parent!)], targets: [sessionNode(s.id)] })),
      })),
    })),
  };
}

function placed(n: ElkNode): Placed {
  return { id: n.id, x: n.x ?? 0, y: n.y ?? 0, width: n.width ?? 0, height: n.height ?? 0 };
}

/** layoutFleet positions every box and card; child positions are relative
 * to their parent, as React Flow's sub-flows expect. */
export async function layoutFleet(data: FleetData): Promise<FleetLayout> {
  const out = await new ELK().layout(graphOf(data));
  const layout: FleetLayout = { machines: [], projects: [], sessions: [] };
  for (const m of out.children ?? []) {
    layout.machines.push(placed(m));
    for (const p of m.children ?? []) {
      layout.projects.push({ ...placed(p), parent: m.id });
      for (const s of p.children ?? []) layout.sessions.push({ ...placed(s), parent: p.id });
    }
  }
  return layout;
}
