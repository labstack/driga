import type { Meta, StoryObj } from "@storybook/react-vite";
import type { NodeProps } from "@xyflow/react";
import { expect, fn, userEvent } from "storybook/test";
import { sessions } from "./Fleet-fixtures";
import { FleetSessionNode, type SessionNode } from "./FleetSessionNode";
import type { FleetSession } from "./FleetTypes";

const of = (id: string) => sessions.find(s => s.id === id)!;

/** nodeProps is what React Flow hands a node; outside a canvas the node
 * renders the same card without ports. */
function nodeProps(session: FleetSession, extra: Partial<SessionNode["data"]> = {}, selected = false): NodeProps<SessionNode> {
  return {
    id: `s:${session.id}`, type: "session", selected, dragging: false, zIndex: 0, isConnectable: false, draggable: false, selectable: true, deletable: false,
    positionAbsoluteX: 0, positionAbsoluteY: 0,
    data: { session, subagents: 0, shared: [], dimmed: false, onOpen: fn(), onReply: fn(), ...extra },
  };
}

/** One session card of B · Topology. */
const meta = {
  id: "fleet-session-node",
  title: "Drafts/Fleet/Session node",
  component: FleetSessionNode,
  args: nodeProps(of("112")),
  parameters: { docs: { description: { component: "The ring's colour is the state; its arcs are the last 30 turns, with waits in yellow and failures in red. Below: id, branch and memory, the tracker strip, the last tool, and Reply when it waits, Open always. ↳ counts subagents; a red ! marks a file another session also edits." } } },
} satisfies Meta<typeof FleetSessionNode>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Working: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByText("Fix the flaky lifeline test")).toBeVisible();
    await expect(canvas.getByRole("img", { name: "working: 28 of 30 turns ok" })).toBeInTheDocument();
    await expect(canvas.queryByRole("button", { name: "Reply" })).toBeNull();
  },
};

export const Waiting: Story = {
  args: nodeProps(of("113"), { subagents: 1 }),
  play: async ({ canvas, args }) => {
    await expect(canvas.getByText("↳1")).toBeVisible();
    await userEvent.click(canvas.getByRole("button", { name: "Reply" }));
    await expect(args.data.onReply).toHaveBeenCalledWith(of("113"));
  },
};

export const StalledWithOverlap: Story = {
  args: nodeProps(of("090"), { shared: ["internal/lifeline/clock.go"] }),
  play: async ({ canvas }) => { await expect(canvas.getByTitle("Also edited elsewhere: clock.go")).toBeVisible(); },
};

export const Finished: Story = {
  args: nodeProps(of("121")),
  play: async ({ canvas }) => { await expect(canvas.getByRole("img", { name: "finished: 9 of 9 turns ok" })).toBeInTheDocument(); },
};

export const Inferred: Story = {
  args: nodeProps(of("080")),
  play: async ({ canvas }) => { await expect(canvas.getByRole("img", { name: "working, inferred: 30 of 30 turns ok" })).toBeInTheDocument(); },
};

export const Selected: Story = {
  args: nodeProps(of("099"), {}, true),
  play: async ({ canvas }) => { await expect(canvas.getByText("Check the pager API").closest("[data-selected]")).toHaveAttribute("data-selected", "true"); },
};

export const Dimmed: Story = {
  args: nodeProps(of("107"), { dimmed: true }),
  play: async ({ canvas }) => { await expect(canvas.getByText("Harbor docs for the CLI").closest("[data-dimmed]")).toHaveAttribute("data-dimmed", "true"); },
};
