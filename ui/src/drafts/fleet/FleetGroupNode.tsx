import "./FleetTopology.css";
import { Badge, ColorSwatch, Group, Text } from "@mantine/core";
import { Desktop, FolderSimple, HardDrives, Laptop, type Icon } from "@phosphor-icons/react";
import type { Node, NodeProps } from "@xyflow/react";
import type { FleetMachine } from "./FleetTypes";

export type GroupNodeData = {
  kind: "machine" | "project";
  title: string;
  caption: string;
  icon: FleetMachine["kind"] | "project";
  local?: boolean;
  /** Machines only; an unreachable one has a dashed box and a hollow dot. */
  reachable?: boolean;
  dimmed: boolean;
};

export type GroupNode = Node<GroupNodeData, "machine" | "project">;

const icons: Record<GroupNodeData["icon"], Icon> = { desktop: Desktop, laptop: Laptop, server: HardDrives, project: FolderSimple };

/** FleetGroupNode is a machine or project box on the topology canvas: a
 * bordered group with its name and a caption in the top-left, sized by the
 * layout to hold what it contains. Machine boxes sit on the inset surface,
 * project boxes on the raised one, so the nesting reads without lines. */
export function FleetGroupNode({ data, selected }: NodeProps<GroupNode>) {
  const Glyph = icons[data.icon];
  const reachable = data.reachable !== false;
  return <div className="fleet-group" data-kind={data.kind} data-dimmed={data.dimmed} data-selected={selected} data-reachable={reachable}>
    <Group gap={6} wrap="nowrap" px={10} py={6} style={{ minWidth: 0 }}>
      <Glyph size={14} aria-hidden style={{ flex: "none" }} />
      <Text fz="sm" fw={600} ff="monospace" style={{ whiteSpace: "nowrap" }}>{data.title}</Text>
      {data.local && <Badge size="xs" variant="default" style={{ flex: "none" }}>local</Badge>}
      {data.kind === "machine" && <ColorSwatch size={7} color={reachable ? "var(--mantine-color-success-filled)" : "transparent"} withShadow={false} aria-hidden style={{ flex: "none", border: reachable ? undefined : "1px solid var(--driga-muted)" }} />}
      <Text fz="xs" c="dimmed" ff="monospace" style={{ whiteSpace: "nowrap" }}>{data.caption}</Text>
    </Group>
  </div>;
}
