import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent } from "storybook/test";
import { busy } from "./Fleet-fixtures";
import { FleetMachineStrip } from "./FleetMachineStrip";
import { groupFleet } from "./FleetModel";

/** The row of machine cards above either Fleet view. */
const meta = {
  id: "fleet-machine-strip",
  title: "Drafts/Fleet/Machine strip",
  component: FleetMachineStrip,
  args: { groups: groupFleet(busy), selected: null, onSelect: fn() },
  parameters: { docs: { description: { component: "Selecting a card narrows the view below to that machine; selecting it again clears the selection. The strip scrolls sideways when the window is narrower than the fleet." } } },
} satisfies Meta<typeof FleetMachineStrip>;
export default meta;
type Story = StoryObj<typeof meta>;

export const ThreeMachines: Story = {
  play: async ({ canvas, args }) => {
    await expect(canvas.getByRole("list", { name: "Machines" })).toBeVisible();
    await expect(canvas.getAllByRole("button")).toHaveLength(3);
    await userEvent.click(canvas.getByRole("button", { name: /^build-01/ }));
    await expect(args.onSelect).toHaveBeenCalledWith("build-01");
  },
};

export const OneSelected: Story = {
  args: { selected: "build-01" },
  play: async ({ canvas, args }) => {
    const card = canvas.getByRole("button", { name: /^build-01/ });
    await expect(card).toHaveAttribute("aria-pressed", "true");
    await userEvent.click(card);
    await expect(args.onSelect).toHaveBeenCalledWith(null);
  },
};
