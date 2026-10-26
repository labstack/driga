import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect } from "storybook/test";
import { Heatmap } from "./Heatmap";
import { activity } from "./Heatmap-fixtures";

/** Projects by hour on a single-hue ramp of the accent. */
const meta = {
  id: "widgets-heatmap",
  title: "Drafts/Widgets/Heatmap",
  component: Heatmap,
  args: activity,
  decorators: [Story => <Box maw={720}><Story /></Box>],
  parameters: { docs: { description: { component: "One cell per project and hour of the last day, its depth the tool calls in that hour, on a ramp from 8% to 100% of the accent. One hue, not a rainbow, so the eye reads depth as amount; the ramp under the chart says which way is busier, and the tooltip gives the count." } } },
} satisfies Meta<typeof Heatmap>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Day: Story = { play: async ({ canvas }) => { await expect(canvas.getByRole("img", { name: "Tool calls per project and hour, last 24 hours" })).toBeInTheDocument(); } };
