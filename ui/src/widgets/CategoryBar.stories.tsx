import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect } from "storybook/test";
import { CategoryBar } from "./CategoryBar";
import * as f from "./CategoryBar-fixtures";

/** One 100% bar split into named segments, with a threshold marker. */
const meta = {
  id: "widgets-category-bar",
  title: "Components/Category bar",
  component: CategoryBar,
  args: f.memorySplit,
  decorators: [Story => <Box maw={480}><Story /></Box>],
  parameters: { docs: { description: { component: "Segments in the categorical slots (or a state color when the bar encodes state), 1 px gaps between them, the free part of the whole as track, and a marker at a threshold. The legend carries names and values, so the bar reads without color. Compare Memory split (categorical, with a 90% marker) and Sessions by state (state colors)." } } },
} satisfies Meta<typeof CategoryBar>;
export default meta;
type Story = StoryObj<typeof meta>;

export const MemorySplit: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("progressbar", { name: "Sessions: 10 GB" })).toBeInTheDocument();
    await expect(canvas.getByText("90% at 32 GB")).toBeVisible();
    await expect(canvas.getByRole("list", { name: "Memory on studio by owner" })).toBeInTheDocument();
  },
};

export const SessionsByState: Story = {
  args: { ...f.sessionsByState, total: undefined, marker: undefined },
  play: async ({ canvas }) => { await expect(canvas.getByRole("progressbar", { name: "working: 7" })).toBeInTheDocument(); },
};
