import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, waitFor } from "storybook/test";
import { busy } from "./Fleet-fixtures";
import { FleetCompare } from "./FleetCompare";

/** Both Fleet variants over the same fleet, stacked, for the choice. */
const meta = {
  id: "fleet-compare",
  title: "Drafts/Fleet/Compare",
  component: FleetCompare,
  args: { ...busy, onSelect: fn(), onOpen: fn(), onReply: fn() },
  decorators: [Story => <Box p="md"><Story /></Box>],
  globals: { viewport: { value: "desktop", isRotated: false } },
  parameters: {
    layout: "fullscreen",
    docs: {
      story: { inline: false, iframeHeight: 1760 },
      description: { component: "A · Sankey answers memory and state in one calm picture that never reshuffles, needs no extra library, and has a fully accessible Tree mode; its relations (spawn, same file) are glyphs and inspector rows rather than lines. B · Topology shows each session as a rich card with a status ring and tracker strip, draws spawn and same-file edges as lines, isolates a neighbourhood on hover and lets the owner pan and zoom; it costs React Flow plus ELK, loaded only when opened, and fourteen cards fit a 1440 window at about 0.78 zoom. Each variant keeps its own selection here." },
    },
  },
} satisfies Meta<typeof FleetCompare>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Stacked: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByText("A · Sankey")).toBeVisible();
    await expect(canvas.getByText("B · Topology")).toBeVisible();
    await expect(canvas.getByRole("img", { name: /Fleet flow/ })).toBeVisible();
    const map = await canvas.findByRole("application", { name: "Fleet map" });
    await waitFor(() => expect(map).toHaveAttribute("aria-busy", "false"));
  },
};
