import { Group, Stack } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect } from "storybook/test";
import { Measure, Sparkline } from "./Measure";

const GB = 1024 ** 3;
const trend = Array.from({ length: 60 }, (_, i) => 24 * GB + ((i * 7) % 11) * 0.25 * GB);

/** A number with its unit at a smaller size, as Canon's stat tile has it,
 * and the sparkline beside a metric. */
const meta = {
  id: "components-measure",
  title: "Components/Measure",
  component: Measure,
  args: { n: 25 * GB, unit: "bytes" },
} satisfies Meta<typeof Measure>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Bytes: Story = { play: async ({ canvas }) => { await expect(canvas.getByText("GB")).toBeVisible(); } };
export const OutOfTotal: Story = { args: { n: 25 * GB, unit: "bytes", total: 36 * GB }, play: async ({ canvas }) => { await expect(canvas.getByText(/\/ 36/)).toBeVisible(); } };
export const Count: Story = { args: { n: 11, unit: "count" } };
export const NoReading: Story = { args: { n: null }, play: async ({ canvas }) => { await expect(canvas.getByText("–")).toBeVisible(); } };
export const Sizes: Story = { render: () => <Group align="flex-end"><Measure n={3} size={17} /><Measure n={3} /><Measure n={3} size={40} /></Group> };
export const WithSparkline: Story = { render: () => <Stack w={240} gap={4}><Measure n={trend[59]} unit="bytes" size={17} /><div style={{ height: 26 }}><Sparkline trend={trend} title="Memory used" /></div></Stack> };
