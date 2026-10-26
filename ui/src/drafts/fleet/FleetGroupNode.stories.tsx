import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import type { NodeProps } from "@xyflow/react";
import { expect } from "storybook/test";
import { FleetGroupNode, type GroupNode } from "./FleetGroupNode";

function nodeProps(data: GroupNode["data"], selected = false): NodeProps<GroupNode> {
  return { id: "box", type: data.kind, selected, dragging: false, zIndex: 0, isConnectable: false, draggable: false, selectable: true, deletable: false, positionAbsoluteX: 0, positionAbsoluteY: 0, data };
}

/** A machine or project box of B · Topology. */
const meta = {
  id: "fleet-group-node",
  title: "Drafts/Fleet/Group node",
  component: FleetGroupNode,
  args: nodeProps({ kind: "machine", title: "studio", caption: "25 GB / 36 GB", icon: "desktop", local: true, reachable: true, dimmed: false }),
  decorators: [Story => <Box w={420} h={160}><Story /></Box>],
} satisfies Meta<typeof FleetGroupNode>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Machine: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByText("studio")).toBeVisible();
    await expect(canvas.getByText("local")).toBeVisible();
  },
};

export const UnreachableMachine: Story = {
  args: nodeProps({ kind: "machine", title: "laptop-02", caption: "last seen 14m ago", icon: "laptop", reachable: false, dimmed: false }),
  play: async ({ canvas }) => { await expect(canvas.getByText("laptop-02").closest("[data-reachable]")).toHaveAttribute("data-reachable", "false"); },
};

export const Project: Story = {
  args: nodeProps({ kind: "project", title: "atlas", caption: "3 sessions · 1021 MB", icon: "project", dimmed: false }),
  play: async ({ canvas }) => { await expect(canvas.getByText("atlas")).toBeVisible(); },
};

export const SelectedProject: Story = {
  args: nodeProps({ kind: "project", title: "beacon", caption: "3 sessions · 2.3 GB", icon: "project", dimmed: false }, true),
  play: async ({ canvas }) => { await expect(canvas.getByText("beacon").closest("[data-selected]")).toHaveAttribute("data-selected", "true"); },
};
