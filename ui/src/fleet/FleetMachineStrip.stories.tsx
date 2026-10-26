import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent } from "storybook/test";
import { minutes } from "../widgets/TrackerBar-fixtures";
import { busy } from "./Fleet-fixtures";
import { FleetMachineStrip } from "./FleetMachineStrip";
import { FleetRelationships } from "./FleetModel";

/** The row of machine cards above the Fleet view. */
const meta = {
  id: "fleet-machine-strip",
  title: "Components/Fleet/Machine strip",
  component: FleetMachineStrip,
  args: { groups: new FleetRelationships(busy).groups, selected: null, onSelect: fn() },
  parameters: { docs: { description: { component: "The machine card as a toggle: selecting one narrows the view below to that machine, selecting it again clears the selection. The strip scrolls sideways when the window is narrower than the fleet. With polls the app has kept, each card carries its strip." } } },
} satisfies Meta<typeof FleetMachineStrip>;
export default meta;
type Story = StoryObj<typeof meta>;

export const ThreeMachines: Story = {
  play: async ({ canvas, args }) => {
    await expect(canvas.getByRole("list", { name: "Machines" })).toBeVisible();
    await expect(canvas.getAllByRole("button")).toHaveLength(3);
    await userEvent.click(canvas.getByRole("button", { name: /^build-01/ }));
    await expect(args.onSelect).toHaveBeenCalledWith("build-01");
    await userEvent.unhover(canvas.getByRole("button", { name: /^build-01/ }));
    canvas.getByRole("button", { name: /^build-01/ }).blur();
  },
};

export const OneSelected: Story = {
  args: { selected: "build-01" },
  play: async ({ canvas, args }) => {
    const card = canvas.getByRole("button", { name: /^build-01/ });
    await expect(card).toHaveAttribute("aria-pressed", "true");
    await userEvent.click(card);
    await expect(args.onSelect).toHaveBeenCalledWith(null);
    await userEvent.unhover(card);
    card.blur();
  },
};

export const WithStrips: Story = {
  name: "With polls kept",
  args: { strips: { studio: { cells: minutes(31, { waits: 2 }).slice(-30), from: "30 polls", to: "now", label: "studio, last 30 polls" } } },
  play: async ({ canvas }) => { await expect(canvas.getByRole("img", { name: /studio, last 30 polls/ })).toBeInTheDocument(); },
};
