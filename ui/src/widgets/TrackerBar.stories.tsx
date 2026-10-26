import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, within } from "storybook/test";
import { TrackerBar } from "./TrackerBar";
import * as f from "./TrackerBar-fixtures";

/** A row of state cells with one tooltip that follows the pointer. */
const meta = {
  id: "widgets-tracker-bar",
  title: "Components/Tracker bar",
  component: TrackerBar,
  args: { cells: f.busy, label: "studio, last 90 minutes", from: "90m", to: "now" },
  decorators: [Story => <Box maw={360}><Story /></Box>],
  parameters: { docs: { description: { component: "One cell per minute, poll or turn, the worst state in it. Quiet cells stay neutral, a step darker when something ran; only waiting (yellow) and stuck (red) take color, and a cell Driga could not see is hollow, so a calm machine reads as a calm strip rather than a green one. Ninety DOM cells cost less than a canvas, and one floating tooltip serves them all. A screen reader hears the counts. Compare Busy, Troubled (a stuck run), Quiet and Unreachable; Turns is the same bar over a session's last 30 turns." } } },
} satisfies Meta<typeof TrackerBar>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Busy: Story = {
  play: async ({ canvas, userEvent }) => {
    const bar = canvas.getByRole("img", { name: /studio, last 90 minutes/ });
    await expect(bar).toBeInTheDocument();
    await userEvent.hover(bar);
    await expect(await within(document.body).findByText(/working|waiting|idle/)).toBeVisible();
    await userEvent.unhover(bar);
  },
};

export const Troubled: Story = { args: { cells: f.troubled, label: "build-01, last 90 minutes" }, play: async ({ canvas }) => { await expect(canvas.getByRole("img", { name: /12 stuck/ })).toBeInTheDocument(); } };
export const Quiet: Story = { args: { cells: f.quiet, label: "laptop-02, last 90 minutes" }, play: async ({ canvas }) => { await expect(canvas.getByRole("img", { name: /quiet/ })).toBeInTheDocument(); } };
export const Unreachable: Story = { args: { cells: f.unreachable, label: "laptop-02, last 90 minutes" }, play: async ({ canvas }) => { await expect(canvas.getByRole("img", { name: /14 unseen/ })).toBeInTheDocument(); } };
export const Turns: Story = {
  args: { cells: f.turns, label: "Last 30 turns", summary: "25 went through, 3 waited, 2 failed", from: "turn 1", to: "turn 30", height: 8 },
  play: async ({ canvas }) => { await expect(canvas.getByRole("img", { name: "Last 30 turns: 25 went through, 3 waited, 2 failed" })).toBeInTheDocument(); await expect(canvas.getByText("turn 30")).toBeVisible(); },
};
