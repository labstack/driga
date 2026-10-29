import { Group } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect } from "storybook/test";
import { RingGauge } from "./RingGauge";
import * as f from "./RingGauge-fixtures";

/** The bar gauge's round form, for the memory and disk pair only. */
const meta = {
  id: "widgets-ring-gauge",
  title: "Components/Ring gauge",
  component: RingGauge,
  args: f.memory,
  parameters: { docs: { description: { component: "A 44 px ring, 4 px thick, the share in its centre and the value over the total beside it, drawn by Mantine's RingProgress at no canvas cost. Same thresholds as the bar gauge. The ring is only for the Pressure pair, memory and disk side by side; the bar is the gauge everywhere else." } } },
} satisfies Meta<typeof RingGauge>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Memory: Story = { play: async ({ canvas }) => { await expect(canvas.getByRole("img", { name: "Memory: 25 GB / 36 GB, 69%" })).toBeInTheDocument(); } };
export const Hot: Story = { args: f.memoryHot, play: async ({ canvas }) => { await expect(canvas.getByText("94%")).toBeVisible(); } };
export const Disk: Story = { args: f.disk, play: async ({ canvas }) => { await expect(canvas.getByText("41%")).toBeVisible(); } };
export const Caption: Story = { args: f.diskWarm, play: async ({ canvas }) => { await expect(canvas.getByText("760 GB · 234 GB free")).toBeVisible(); } };
export const LabelFits: Story = {
  render: () => <Group gap="xl">{[36, 44].map(size => <Group key={size} gap="sm">{[0, 9, 75, 86, 99, 100].map(value => <RingGauge key={value} value={value} max={100} format={String} label="Memory" size={size} />)}</Group>)}</Group>,
  play: async ({ canvas }) => {
    await document.fonts.ready;
    const rings = canvas.getAllByRole("img");
    expect(rings).toHaveLength(12);
    for (const ring of rings) {
      const circle = ring.querySelector("circle")!;
      const innerDiameter = Number(circle.getAttribute("r")) * 2 - Number(circle.getAttribute("stroke-width"));
      const text = ring.querySelector(".mantine-Text-root")!;
      const range = document.createRange();
      range.selectNodeContents(text);
      expect(range.getBoundingClientRect().width, ring.getAttribute("aria-label")!).toBeLessThanOrEqual(innerDiameter);
    }
    expect(canvas.getAllByRole("img", { name: "Memory: 100 / 100, 100%" })).toHaveLength(2);
  },
};
export const Pair: Story = {
  render: () => <Group gap="xl"><RingGauge {...f.memory} /><RingGauge {...f.disk} /></Group>,
  parameters: { docs: { description: { story: "The Pressure pair: memory and disk on one machine, the one place the ring is used." } } },
  play: async ({ canvas }) => { await expect(canvas.getAllByRole("img")).toHaveLength(2); },
};
