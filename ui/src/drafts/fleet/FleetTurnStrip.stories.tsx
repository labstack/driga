import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect } from "storybook/test";
import { sessions } from "./Fleet-fixtures";
import { FleetTurnStrip } from "./FleetTurnStrip";

const of = (id: string) => sessions.find(s => s.id === id)!;

/** The 30-cell tracker of a session's last turns. */
const meta = {
  id: "fleet-turn-strip",
  title: "Drafts/Fleet/Turn strip",
  component: FleetTurnStrip,
  args: { session: of("104") },
  decorators: [Story => <Box w={240}><Story /></Box>],
} satisfies Meta<typeof FleetTurnStrip>;
export default meta;
type Story = StoryObj<typeof meta>;

export const WithFailures: Story = {
  play: async ({ canvas }) => { await expect(canvas.getByRole("img", { name: "Last 30 turns: 26 ok, 0 waited, 4 failed" })).toBeInTheDocument(); },
};

export const WithWaits: Story = {
  args: { session: of("113") },
  play: async ({ canvas }) => { await expect(canvas.getByRole("img", { name: "Last 22 turns: 20 ok, 2 waited, 0 failed" })).toBeInTheDocument(); },
};

export const Young: Story = {
  args: { session: of("118") },
  play: async ({ canvas }) => { await expect(canvas.getByRole("img", { name: /Last 7 turns/ })).toBeInTheDocument(); },
};
