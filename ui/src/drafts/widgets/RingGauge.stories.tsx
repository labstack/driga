import { Group } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect } from "storybook/test";
import { RingGauge } from "./RingGauge";
import * as f from "./RingGauge-fixtures";

/** The bar gauge's round form for memory and disk on a machine card. */
const meta = {
  id: "widgets-ring-gauge",
  title: "Drafts/Widgets/Ring gauge",
  component: RingGauge,
  args: f.memory,
  parameters: { docs: { description: { component: "A 44 px ring, 4 px thick, the share in its centre and the value over the total beside it, drawn by Mantine's RingProgress at no canvas cost. Same thresholds as the bar gauge. Compare Memory with Hot, and the Pair as a machine card would show them." } } },
} satisfies Meta<typeof RingGauge>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Memory: Story = { play: async ({ canvas }) => { await expect(canvas.getByRole("img", { name: "Memory: 25 GB / 36 GB, 69%" })).toBeInTheDocument(); } };
export const Hot: Story = { args: f.memoryHot, play: async ({ canvas }) => { await expect(canvas.getByText("94%")).toBeVisible(); } };
export const Disk: Story = { args: f.disk, play: async ({ canvas }) => { await expect(canvas.getByText("41%")).toBeVisible(); } };
export const Caption: Story = { args: f.diskWarm, play: async ({ canvas }) => { await expect(canvas.getByText("760 GB · 234 GB free")).toBeVisible(); } };
export const Pair: Story = {
  render: () => <Group gap="xl"><RingGauge {...f.memory} /><RingGauge {...f.disk} /></Group>,
  play: async ({ canvas }) => { await expect(canvas.getAllByRole("img")).toHaveLength(2); },
};
