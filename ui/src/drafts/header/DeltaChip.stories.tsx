import { Group } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect } from "storybook/test";
import { DeltaChip } from "./DeltaChip";

/** The change chip both header drafts use: ▲ or ▼, the amount and the
 * window, tinted by whether the change is good news. */
const meta = {
  id: "header-delta-chip",
  title: "Drafts/Header/Delta chip",
  component: DeltaChip,
  args: { delta: -1.8, amount: "1.8 GB", polarity: "good", window: "1h" },
  decorators: [Story => <Group gap="sm"><Story /></Group>],
  parameters: { docs: { description: { component: "A delta against the previous period, stated in text: ▲ or ▼, the amount and the window. Green when the change is good news, red when it is bad, gray when it is neutral; the text itself stays in the text color so Latte passes AA." } } },
} satisfies Meta<typeof DeltaChip>;
export default meta;
type Story = StoryObj<typeof meta>;

export const MemoryFell: Story = { name: "Memory fell", play: async ({ canvas }) => { await expect(canvas.getByText(/down/)).toBeInTheDocument(); await expect(canvas.getByText(/1.8 GB/)).toBeVisible(); } };
export const StuckRose: Story = { name: "Stuck rose", args: { delta: 1, amount: "1", polarity: "bad" }, play: async ({ canvas }) => { await expect(canvas.getByText(/up/)).toBeInTheDocument(); } };
export const ReviewRose: Story = { name: "To review rose, neutral", args: { delta: 4, amount: "4", polarity: "neutral", window: undefined }, play: async ({ canvas }) => { await expect(canvas.getByText("4")).toBeVisible(); } };
export const Steady: Story = { args: { delta: 0, amount: "0", polarity: "neutral" }, play: async ({ canvas }) => { await expect(canvas.getByText(/steady/)).toBeVisible(); } };
