import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect } from "storybook/test";
import { FleetEmpty } from "./FleetEmpty";

/** The Fleet with no machines. */
const meta = {
  id: "fleet-empty",
  title: "Components/Fleet/Empty",
  component: FleetEmpty,
} satisfies Meta<typeof FleetEmpty>;
export default meta;
type Story = StoryObj<typeof meta>;

export const NoMachines: Story = {
  play: async ({ canvas }) => { await expect(canvas.getByText("No machines yet")).toBeVisible(); },
};
