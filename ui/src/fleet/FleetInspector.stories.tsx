import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent } from "storybook/test";
import { bare, busy, waitingSession } from "./Fleet-fixtures";
import { FleetInspector } from "./FleetInspector";

/** The panel beside the Fleet view: the selected session, machine or
 * project, with its actions. */
const meta = {
  id: "fleet-inspector",
  title: "Components/Fleet/Inspector",
  component: FleetInspector,
  args: { data: busy, selection: null, onSelect: fn(), onOpen: fn(), onReply: fn() },
  decorators: [Story => <Box h={640} w={360} style={{ display: "flex" }}><Story /></Box>],
  parameters: { docs: { description: { component: "A session shows its state as a pill, where it runs, why it waits in a tinted box with a rule, its last tool, counters, the 30-turn strip, files (with who else edits them), subagents or parent, and Reply beside Open. A machine shows memory, its agent memory by owner, sessions by state, projects and leftovers. A project shows the machines it runs on and its sessions. Nothing opens in a modal. What the service does not send yet is absent, not empty." } } },
} satisfies Meta<typeof FleetInspector>;
export default meta;
type Story = StoryObj<typeof meta>;

export const NothingSelected: Story = {
  play: async ({ canvas }) => { await expect(canvas.getByText("Nothing selected")).toBeVisible(); },
};

export const WaitingSession: Story = {
  args: { selection: { kind: "session", id: waitingSession.id } },
  play: async ({ canvas, args }) => {
    await expect(canvas.getByRole("heading", { name: waitingSession.title })).toBeVisible();
    await expect(canvas.getByText("Needs you")).toBeVisible();
    await expect(canvas.getByRole("img", { name: /Last 30 turns: 20 went through, 2 waited, 0 failed/ })).toBeInTheDocument();
    await userEvent.click(canvas.getByRole("button", { name: "Reply" }));
    await expect(args.onReply).toHaveBeenCalledWith(waitingSession);
    await userEvent.unhover(canvas.getByRole("button", { name: "Reply" }));
  },
};

export const WorkingSessionWithOverlap: Story = {
  args: { selection: { kind: "session", id: "112" } },
  play: async ({ canvas, args }) => {
    await expect(canvas.getByRole("heading", { name: "Fix the flaky lifeline test" })).toBeVisible();
    await expect(canvas.getByText("also #090")).toBeVisible();
    await expect(canvas.queryByRole("button", { name: "Reply" })).toBeNull();
    await userEvent.click(canvas.getByRole("button", { name: "Open" }));
    await expect(args.onOpen).toHaveBeenCalled();
    await userEvent.unhover(canvas.getByRole("button", { name: "Open" }));
  },
};

export const StuckSession: Story = {
  args: { selection: { kind: "session", id: "104" } },
  play: async ({ canvas }) => {
    await expect(canvas.getByLabelText("stuck, inferred")).toBeVisible();
    await expect(canvas.getByText("busy, no output for 23m, CPU idle")).toBeVisible();
    await expect(canvas.getByRole("img", { name: "Last 30 turns: 26 went through, 0 waited, 4 failed" })).toBeInTheDocument();
  },
};

export const AsTheServiceSendsIt: Story = {
  name: "As the service sends it",
  args: { data: bare, selection: { kind: "session", id: "104" } },
  parameters: { docs: { description: { story: "No turns, no files, no start time: the strip, the Files list and the counters that would count them are absent." } } },
  play: async ({ canvas }) => {
    await expect(canvas.queryByRole("img")).toBeNull();
    await expect(canvas.queryByText("Files")).toBeNull();
    await expect(canvas.getByText(/quiet 23m$/)).toBeVisible();
  },
};

export const Subagent: Story = {
  args: { selection: { kind: "session", id: "099" } },
  play: async ({ canvas, args }) => {
    await expect(canvas.getByText("Spawned by")).toBeVisible();
    await userEvent.click(canvas.getByRole("button", { name: /Beacon alert routing/ }));
    await expect(args.onSelect).toHaveBeenCalledWith({ kind: "session", id: "095" });
    await userEvent.unhover(canvas.getByRole("button", { name: /Beacon alert routing/ }));
  },
};

export const Machine: Story = {
  args: { selection: { kind: "machine", id: "studio" } },
  play: async ({ canvas, args }) => {
    await expect(canvas.getByRole("heading", { name: "studio" })).toBeVisible();
    await expect(canvas.getByText("3 processes · 1.8 GB reclaimable")).toBeVisible();
    await expect(canvas.getByRole("list", { name: "Agent memory on studio by owner" })).toBeInTheDocument();
    await userEvent.click(canvas.getByRole("button", { name: /^atlas/ }));
    await expect(args.onSelect).toHaveBeenCalledWith({ kind: "project", id: "atlas" });
    await userEvent.unhover(canvas.getByRole("button", { name: /^atlas/ }));
  },
};

export const UnreachableMachine: Story = {
  args: { selection: { kind: "machine", id: "laptop-02" } },
  play: async ({ canvas }) => { await expect(canvas.getByText("unreachable · last seen 14m ago")).toBeVisible(); },
};

export const Project: Story = {
  args: { selection: { kind: "project", id: "atlas" } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("heading", { name: "atlas" })).toBeVisible();
    await expect(canvas.getByText("studio · build-01")).toBeVisible();
    await expect(canvas.getAllByRole("button")).toHaveLength(5);
  },
};
