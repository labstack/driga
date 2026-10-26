import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn } from "storybook/test";
import { Gallery } from "./Gallery";
import * as f from "./Gallery-fixtures";

/** Every widget draft on one board, as Home would lay them out. */
const meta: Meta<typeof Gallery> = {
  id: "widgets-gallery",
  title: "Drafts/Widgets/Gallery",
  component: Gallery,
  args: { ...f.busy, onTile: fn(), onRange: fn(), onChartAction: fn(), onOpenEntry: fn(), onApproveEntry: fn(), onDismissEntry: fn(), onOpenSession: fn(), onApproveSession: fn(), onOpenMachine: fn(), onReclaim: fn(), onSelectRank: fn() },
  decorators: [Story => <Box p="md"><Story /></Box>],
  parameters: { layout: "fullscreen", docs: { story: { inline: false, iframeHeight: 1900 }, description: { component: "The widgets together and in proportion: the KPI strip, the memory chart beside its attribution, the three machines, the inbox beside the sessions, activity beside the timeline, and the smaller parts in a last row. Busy is a fleet with things to do; Calm is the same board when nothing needs you, with quiet zeros and the all-clear row." } } },
};
export default meta;
type Story = StoryObj<typeof meta>;

export const Busy: Story = {
  play: async ({ args, canvas, userEvent, step }) => {
    await expect(canvas.getByRole("heading", { name: "Inbox" })).toBeVisible();
    await expect(canvas.getByRole("heading", { name: "Sessions" })).toBeVisible();
    await expect(canvas.getAllByRole("img", { name: /last 90 minutes/ })).toHaveLength(3);
    await step("A tile filters", async () => {
      const tile = canvas.getByRole("button", { name: /Need you/ });
      await userEvent.click(tile);
      await expect(args.onTile).toHaveBeenCalledWith("Need you");
      await userEvent.unhover(tile);
    });
  },
};

export const Calm: Story = {
  args: f.calm,
  play: async ({ canvas }) => {
    await expect(canvas.getAllByRole("status")[0]).toHaveTextContent("Nothing needs you");
    await expect(canvas.getByRole("button", { name: /Need you/ })).not.toHaveAttribute("data-attention");
  },
};
