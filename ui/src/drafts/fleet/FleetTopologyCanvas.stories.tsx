import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, waitFor, within } from "storybook/test";
import { busy, oneMachine } from "./Fleet-fixtures";
import { FleetTopologyCanvas } from "./FleetTopologyCanvas";

/** The canvas of B · Topology on its own, loaded eagerly here. */
const meta = {
  id: "fleet-topology-canvas",
  title: "Drafts/Fleet/Topology canvas",
  component: FleetTopologyCanvas,
  args: { data: busy, selection: null, onSelect: fn(), onOpen: fn(), onReply: fn() },
  decorators: [Story => <Box h={620} style={{ display: "flex" }}><Story /></Box>],
  globals: { viewport: { value: "desktop", isRotated: false } },
  parameters: { docs: { story: { inline: false, iframeHeight: 660 } } },
} satisfies Meta<typeof FleetTopologyCanvas>;
export default meta;
type Story = StoryObj<typeof meta>;

async function mapReady(canvas: ReturnType<typeof within>) {
  const map = await canvas.findByRole("application", { name: "Fleet map" });
  await waitFor(() => expect(map).toHaveAttribute("aria-busy", "false"));
  return map;
}

export const Busy: Story = {
  play: async ({ canvas }) => {
    const inMap = within(await mapReady(canvas));
    await expect(inMap.getAllByRole("group", { name: /, (working|waiting|possibly stalled|idle|finished), / })).toHaveLength(14);
    await expect(inMap.getByRole("button", { name: "Fit the fleet" })).toBeVisible();
  },
};

export const OneMachineWithMinimap: Story = {
  args: { data: oneMachine, minimap: true },
  play: async ({ canvas }) => {
    await mapReady(canvas);
    await expect(canvas.getByRole("img", { name: "Minimap of the fleet" })).toBeVisible();
  },
};
