import "@xyflow/react/dist/style.css";
import "./FleetTopology.css";
import { ActionIcon, Group, useComputedColorScheme } from "@mantine/core";
import { CornersOut, Minus, Plus } from "@phosphor-icons/react";
import { Background, BackgroundVariant, MarkerType, MiniMap, Panel, ReactFlow, ReactFlowProvider, useReactFlow, type Edge, type Node, type NodeTypes } from "@xyflow/react";
import { useEffect, useMemo, useState } from "react";
import { bytes, relative } from "../../panels/format";
import { surfaces, tones } from "../../theme/tokens";
import { FleetGroupNode, type GroupNode } from "./FleetGroupNode";
import { basename, childrenOf, groupFleet, overlaps, overlapsOf, plural } from "./FleetModel";
import { FleetSessionNode, type SessionNode } from "./FleetSessionNode";
import { layoutFleet, machineNode, projectNode, sessionNode, type FleetLayout } from "./FleetTopologyLayout";
import { sankeyColors } from "./FleetSankeyOption";
import type { FleetActions, FleetData, FleetSelection } from "./FleetTypes";

export type FleetTopologyCanvasProps = FleetActions & { data: FleetData; selection: FleetSelection | null; minimap?: boolean };

type FleetNode = SessionNode | GroupNode;

const nodeTypes: NodeTypes = { session: FleetSessionNode, machine: FleetGroupNode, project: FleetGroupNode };

const fit = { padding: 0.06, maxZoom: 1 };

const aria = {
  "node.a11yDescription.default": "Press Enter to show it in the inspector.",
  "node.a11yDescription.keyboardDisabled": "Press Enter to show it in the inspector.",
  "edge.a11yDescription.default": "A relation between two sessions.",
  "minimap.ariaLabel": "Minimap of the fleet",
};

/** selectionOf reads a node id back into what it stands for. */
export function selectionOf(id: string): FleetSelection | null {
  if (id.startsWith("m:")) return { kind: "machine", id: id.slice(2) };
  if (id.startsWith("p:")) return { kind: "project", id: id.slice(id.indexOf("/") + 1) };
  if (id.startsWith("s:")) return { kind: "session", id: id.slice(2) };
  return null;
}

function isSelected(selection: FleetSelection | null, id: string): boolean {
  const s = selectionOf(id);
  return !!selection && !!s && s.kind === selection.kind && s.id === selection.id;
}

function same(a: FleetSelection | null, b: FleetSelection | null): boolean {
  return a?.kind === b?.kind && a?.id === b?.id;
}

/** graphOf turns the fleet and its layout into React Flow nodes and edges:
 * machine boxes, project boxes inside them, session cards inside those
 * (parents before children, as sub-flows need), spawn edges from a parent
 * to each subagent, and a red dashed edge between sessions editing the same
 * file. Resolved colours go only where SVG markers cannot read a variable. */
function graphOf(data: FleetData, layout: FleetLayout, scheme: "light" | "dark", act: Pick<FleetActions, "onOpen" | "onReply">): { nodes: FleetNode[]; edges: Edge[] } {
  const groups = groupFleet(data);
  const byId = new Map(data.sessions.map(s => [s.id, s]));
  const nodes: FleetNode[] = [];
  for (const g of groups) {
    const m = g.machine;
    const box = layout.machines.find(x => x.id === machineNode(m.id));
    if (!box) continue;
    const count = g.projects.reduce((n, p) => n + p.sessions.length, 0);
    nodes.push({
      id: box.id, type: "machine", position: { x: box.x, y: box.y }, width: box.width, height: box.height, selectable: true, draggable: false, zIndex: 0,
      ariaLabel: `${m.name}${m.local ? ", local" : ""}, ${m.reachable ? "reachable" : `last seen ${relative(m.lastSeen)}`}, ${plural(count, "session")}`,
      data: { kind: "machine", title: m.name, icon: m.kind, local: m.local, reachable: m.reachable, dimmed: false, caption: m.reachable ? `${bytes(m.memoryUsed)} / ${bytes(m.memoryTotal)}` : `last seen ${relative(m.lastSeen)}` },
    });
    for (const p of g.projects) {
      const pbox = layout.projects.find(x => x.id === projectNode(m.id, p.project.id));
      if (!pbox) continue;
      nodes.push({
        id: pbox.id, type: "project", parentId: box.id, extent: "parent", position: { x: pbox.x, y: pbox.y }, width: pbox.width, height: pbox.height, selectable: true, draggable: false, zIndex: 1,
        ariaLabel: `${p.project.name} on ${m.name}, ${plural(p.sessions.length, "session")}`,
        data: { kind: "project", title: p.project.name, icon: "project", dimmed: false, caption: `${plural(p.sessions.length, "session")} · ${bytes(p.memory)}` },
      });
      for (const s of p.sessions) {
        const sbox = layout.sessions.find(x => x.id === sessionNode(s.id));
        if (!sbox) continue;
        nodes.push({
          id: sbox.id, type: "session", parentId: pbox.id, extent: "parent", position: { x: sbox.x, y: sbox.y }, width: sbox.width, height: sbox.height, selectable: true, draggable: false, zIndex: 3,
          ariaLabel: `${s.title}, ${s.state === "stalled" ? "possibly stalled" : s.state}, ${s.agent}, ${bytes(s.memory)}`,
          data: { session: s, subagents: childrenOf(data.sessions, s.id).length, shared: overlapsOf(data.sessions, s.id).flatMap(o => o.files), dimmed: false, onOpen: act.onOpen, onReply: act.onReply },
        });
      }
    }
  }
  // Edges sit above the boxes (z 0 and 1) and below the cards (z 3). They
  // carry no visible label: a card's ! and the inspector name the file, and
  // a label at a bezier's midpoint lands under whatever card is there.
  const edges: Edge[] = [];
  for (const s of data.sessions) {
    const parent = s.parent ? byId.get(s.parent) : undefined;
    if (!parent) continue;
    edges.push({
      id: `spawn:${s.id}`, source: sessionNode(parent.id), target: sessionNode(s.id), type: "smoothstep", zIndex: 2,
      markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14, color: surfaces[scheme].muted },
      style: { stroke: "var(--driga-muted)", strokeWidth: 1.5 },
      ariaLabel: `${parent.title} spawned ${s.title}`,
    });
  }
  for (const o of overlaps(data.sessions)) {
    const [a, b] = [byId.get(o.a)!, byId.get(o.b)!];
    const files = o.files.map(basename).join(", ");
    edges.push({
      id: `overlap:${o.a}:${o.b}`, source: sessionNode(o.a), target: sessionNode(o.b), type: "default", zIndex: 2,
      style: { stroke: "var(--mantine-color-danger-filled)", strokeWidth: 1.5, strokeDasharray: "5 4" },
      ariaLabel: `${a.title} and ${b.title} both edit ${files}`,
    });
  }
  return { nodes, edges };
}

/** neighbourhood lists what stays lit while a node is hovered: the node, the
 * boxes around it, everything inside it, and the sessions its edges reach
 * with their boxes. */
function neighbourhood(nodes: FleetNode[], edges: Edge[], id: string): Set<string> {
  const parent = new Map(nodes.map(n => [n.id, n.parentId]));
  const ancestors = (x: string) => { const out: string[] = []; for (let p = parent.get(x); p; p = parent.get(p)) out.push(p); return out; };
  const lit = new Set([id]);
  for (const n of nodes) if (ancestors(n.id).includes(id)) lit.add(n.id);
  for (const e of edges) { if (e.source === id) lit.add(e.target); if (e.target === id) lit.add(e.source); }
  for (const x of [...lit]) for (const a of ancestors(x)) lit.add(a);
  return lit;
}

/** FleetTopologyCanvas draws the fleet as nested boxes on a React Flow
 * canvas: machines hold projects hold session cards, laid out by ELK so
 * positions never drift. Hovering a card or box isolates its neighbourhood;
 * clicking, or Enter on a focused one, selects it for the inspector. The
 * canvas pans and zooms; nothing drags. The minimap is off unless asked. */
export function FleetTopologyCanvas(props: FleetTopologyCanvasProps) {
  return <ReactFlowProvider><Canvas {...props} /></ReactFlowProvider>;
}

function Canvas({ data, selection, minimap, onSelect, onOpen, onReply }: FleetTopologyCanvasProps) {
  const scheme = useComputedColorScheme("light");
  const { fitView, zoomIn, zoomOut } = useReactFlow();
  const [layout, setLayout] = useState<{ data: FleetData; layout: FleetLayout } | null>(null);
  const [ready, setReady] = useState(false);
  const [hovered, setHovered] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    setReady(false);
    void layoutFleet(data).then(l => { if (live) setLayout({ data, layout: l }); });
    return () => { live = false; };
  }, [data]);
  useEffect(() => {
    if (!layout) return;
    const id = requestAnimationFrame(() => { void fitView({ ...fit, duration: 0 }).then(() => setReady(true)); });
    return () => cancelAnimationFrame(id);
  }, [layout, fitView]);
  const graph = useMemo(() => layout && layout.data === data ? graphOf(data, layout.layout, scheme, { onOpen, onReply }) : { nodes: [] as FleetNode[], edges: [] as Edge[] }, [data, layout, scheme, onOpen, onReply]);
  const lit = useMemo(() => hovered ? neighbourhood(graph.nodes, graph.edges, hovered) : null, [graph, hovered]);
  const nodes = useMemo(() => graph.nodes.map(n => ({ ...n, selected: isSelected(selection, n.id), data: { ...n.data, dimmed: !!lit && !lit.has(n.id) } }) as FleetNode), [graph, selection, lit]);
  const edges = useMemo(() => graph.edges.map(e => ({ ...e, style: { ...e.style, opacity: lit && !(lit.has(e.source) && lit.has(e.target)) ? 0.15 : 1 } })), [graph, lit]);
  const colors = useMemo(() => sankeyColors(scheme), [scheme]);
  const pick = (id: string | null) => { const next = id ? selectionOf(id) : null; if (!same(next, selection)) onSelect(next); };
  return <div className="fleet-flow">
    <ReactFlow
      aria-label="Fleet map" aria-busy={!ready}
      nodes={nodes as Node[]} edges={edges} nodeTypes={nodeTypes} colorMode={scheme} zIndexMode="manual"
      fitView fitViewOptions={fit} minZoom={0.3} maxZoom={1.5}
      nodesDraggable={false} nodesConnectable={false} elementsSelectable selectNodesOnDrag={false}
      multiSelectionKeyCode={null} deleteKeyCode={null} zoomOnDoubleClick={false}
      proOptions={{ hideAttribution: true }}
      ariaLabelConfig={aria}
      onNodeClick={(_, n) => pick(n.id)}
      onPaneClick={() => pick(null)}
      onNodesChange={changes => { for (const c of changes) if (c.type === "select" && c.selected) pick(c.id); }}
      onNodeMouseEnter={(_, n) => setHovered(n.id)}
      onNodeMouseLeave={() => setHovered(null)}
    >
      <Background variant={BackgroundVariant.Dots} gap={20} size={1} />
      <Panel position="bottom-left">
        <Group gap={2} wrap="nowrap">
          <ActionIcon variant="default" size="sm" aria-label="Zoom in" onClick={() => void zoomIn({ duration: 150 })}><Plus size={14} /></ActionIcon>
          <ActionIcon variant="default" size="sm" aria-label="Zoom out" onClick={() => void zoomOut({ duration: 150 })}><Minus size={14} /></ActionIcon>
          <ActionIcon variant="default" size="sm" aria-label="Fit the fleet" onClick={() => void fitView({ ...fit, duration: 150 })}><CornersOut size={14} /></ActionIcon>
        </Group>
      </Panel>
      {minimap && <MiniMap pannable zoomable position="bottom-right" nodeColor={n => n.type === "session" ? colors[(n.data as SessionNode["data"]).session.state] : "transparent"} nodeStrokeColor={n => n.type === "session" ? "transparent" : tones[scheme].brand} nodeStrokeWidth={2} />}
    </ReactFlow>
  </div>;
}
