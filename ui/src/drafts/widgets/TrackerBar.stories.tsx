import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, within } from "storybook/test";
import { TrackerBar } from "./TrackerBar";
import * as f from "./TrackerBar-fixtures";

/** A row of state cells with one tooltip that follows the pointer. */
const meta = {
  id: "widgets-tracker-bar",
  title: "Drafts/Widgets/Tracker bar",
  component: TrackerBar,
  args: { cells: f.busy, label: "studio, last 90 minutes", from: "90m", to: "now" },
  decorators: [Story => <Box maw={360}><Story /></Box>],
  parameters: { docs: { description: { component: "One cell per minute or poll, the worst state in it: green working, yellow waiting, red stuck, blue idle, grey none. Ninety DOM cells cost less than a canvas, and one floating tooltip serves them all. A screen reader hears the counts. Compare Busy, Troubled (a stuck run) and Quiet; Turns is the same bar over a session's last 90 turns." } } },
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
export const Turns: Story = { args: { cells: f.turns, label: "Last 90 turns", from: "turn 1", to: "turn 90", height: 8 }, play: async ({ canvas }) => { await expect(canvas.getByText("turn 90")).toBeVisible(); } };
