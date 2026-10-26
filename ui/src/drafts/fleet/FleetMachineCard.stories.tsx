import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent } from "storybook/test";
import { busy } from "./Fleet-fixtures";
import { FleetMachineCard } from "./FleetMachineCard";
import { groupFleet } from "./FleetModel";

const groups = groupFleet(busy);
const studio = groups[0], build = groups[1], laptop = groups[2];
const [quiet] = groupFleet({ machines: busy.machines.slice(1, 2), projects: busy.projects, sessions: [] });

/** One machine of the Fleet strip: hostname and reachability, memory used of
 * total, sessions by state, and what is left over. */
const meta = {
  id: "fleet-machine-card",
  title: "Drafts/Fleet/Machine card",
  component: FleetMachineCard,
  args: { group: studio },
  parameters: { docs: { description: { component: "A stacked bar and counts say how many sessions are in each state; the memory bar stays neutral, since yellow and red mean waiting and stuck. An unreachable machine keeps its last known numbers with a hollow dot." } } },
} satisfies Meta<typeof FleetMachineCard>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Local: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByText("studio")).toBeVisible();
    await expect(canvas.getByText("local")).toBeVisible();
    await expect(canvas.getByText("reachable")).toBeVisible();
    await expect(canvas.getByText("3 leftovers · 1.8 GB reclaimable")).toBeVisible();
  },
};

export const Server: Story = {
  args: { group: build },
  play: async ({ canvas }) => { await expect(canvas.getByText("build-01")).toBeVisible(); },
};

export const Unreachable: Story = {
  args: { group: laptop },
  play: async ({ canvas }) => {
    await expect(canvas.getByText("last seen 14m ago")).toBeVisible();
    await expect(canvas.getByText("last known")).toBeVisible();
    await expect(canvas.getByText("no leftovers")).toBeVisible();
  },
};

export const NoSessions: Story = {
  args: { group: quiet },
  play: async ({ canvas }) => { await expect(canvas.getByRole("progressbar", { name: "Memory used 64%" })).toBeInTheDocument(); },
};

export const Selectable: Story = {
  args: { onSelect: fn(), selected: false },
  play: async ({ canvas, args }) => {
    const card = canvas.getByRole("button", { name: /^studio local reachable/ });
    await expect(card).toHaveAttribute("aria-pressed", "false");
    await userEvent.click(card);
    await expect(args.onSelect).toHaveBeenCalledWith("studio");
  },
};

export const Selected: Story = {
  args: { onSelect: fn(), selected: true },
  play: async ({ canvas }) => { await expect(canvas.getByRole("button", { name: /^studio/ })).toHaveAttribute("aria-pressed", "true"); },
};
