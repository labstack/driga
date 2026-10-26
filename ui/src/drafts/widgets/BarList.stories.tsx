import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn } from "storybook/test";
import { BarList } from "./BarList";
import * as f from "./BarList-fixtures";

/** Ranked bars with a name on each and the value in mono. */
const meta = {
  id: "widgets-bar-list",
  title: "Drafts/Widgets/Bar list",
  component: BarList,
  args: { ...f.memoryByProject, onSelect: fn() },
  decorators: [Story => <Box maw={420}><Story /></Box>],
  parameters: { docs: { description: { component: "Each row is a soft accent-tinted bar as long as the item's share of the largest, with the name on the bar and the value right-aligned. Built from Mantine alone, since @mantine/charts' BarsList would bring Recharts as a second chart engine. Rows become buttons when the list offers a drill-down. Compare Memory by project (clickable) and Tokens by session (ranked, long names truncate)." } } },
} satisfies Meta<typeof BarList>;
export default meta;
type Story = StoryObj<typeof meta>;

export const MemoryByProject: Story = {
  play: async ({ args, canvas, userEvent }) => {
    await expect(canvas.getByRole("list", { name: "Memory by project" })).toBeInTheDocument();
    const row = canvas.getByRole("button", { name: /^harbor/ });
    await expect(row).toHaveTextContent("3.9 GB");
    await userEvent.click(row);
    await expect(args.onSelect).toHaveBeenCalledWith("harbor");
    await userEvent.unhover(row);
  },
};

export const TokensBySession: Story = {
  args: { ...f.tokensBySession, onSelect: undefined },
  play: async ({ canvas }) => {
    await expect(canvas.queryByRole("button")).toBeNull();
    await expect(canvas.getByText("412k")).toBeVisible();
    await expect(canvas.getByTitle("Rewrite the dashboard layout tests")).toBeVisible();
  },
};
