import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect } from "storybook/test";
import { Donut } from "./Donut";
import * as f from "./Donut-fixtures";

/** A share of a whole with the whole in its centre. */
const meta = {
  id: "widgets-donut",
  title: "Drafts/Widgets/Donut",
  component: Donut,
  args: f.attribution,
  decorators: [Story => <Box maw={420}><Story /></Box>],
  parameters: { docs: { description: { component: "Fleet memory across the four attribution buckets: sessions, MCP servers, agents without a session, leftovers. The ring carries no labels; the list beside it does, with each value and share in mono, so the chart reads without hovering and without color. The centre holds the total." } } },
} satisfies Meta<typeof Donut>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Attribution: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("img", { name: "Fleet memory by bucket: 29.6 GB agents' memory" })).toBeInTheDocument();
    await expect(canvas.getByText("MCP servers")).toBeVisible();
    await expect(canvas.getByText("18 GB · 62%")).toBeVisible();
  },
};

export const Empty: Story = { args: f.empty, play: async ({ canvas }) => { await expect(canvas.getByRole("img", { name: /0 B no agents/ })).toBeInTheDocument(); } };
