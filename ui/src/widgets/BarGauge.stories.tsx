import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect } from "storybook/test";
import { BarGauge } from "./BarGauge";
import * as f from "./BarGauge-fixtures";

/** A thin pressure bar with threshold ticks at 70% and 90%. */
const meta = {
  id: "widgets-bar-gauge",
  title: "Components/Bar gauge",
  component: BarGauge,
  args: f.calm,
  decorators: [Story => <Box maw={240}><Story /></Box>],
  parameters: { docs: { description: { component: "Used over total in mono on the left, the share on the right, a 4 px track whose fill is the accent below 70%, warning to 90% and danger above, and two ticks on the track at the thresholds. The default gauge everywhere: the header's memory, table cells, the machine card. Compare: Calm, Warm and Hot for the three fills; Bare for a table cell without the caption." } } },
} satisfies Meta<typeof BarGauge>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Calm: Story = { play: async ({ canvas }) => { await expect(canvas.getByRole("progressbar", { name: "Memory on studio: 25 GB of 36 GB, 69%" })).toBeInTheDocument(); await expect(canvas.getByText("69%")).toBeVisible(); } };
export const Warm: Story = { args: f.warm, play: async ({ canvas }) => { await expect(canvas.getByText("78%")).toBeVisible(); } };
export const Hot: Story = { args: f.hot, play: async ({ canvas }) => { await expect(canvas.getByText("94%")).toBeVisible(); } };
export const Disk: Story = { args: f.disk, play: async ({ canvas }) => { await expect(canvas.getByText("41%")).toBeVisible(); } };
export const Bare: Story = { args: { ...f.warm, caption: false, size: 3 }, play: async ({ canvas }) => { await expect(canvas.queryByText("78%")).toBeNull(); await expect(canvas.getByRole("progressbar")).toBeInTheDocument(); } };
