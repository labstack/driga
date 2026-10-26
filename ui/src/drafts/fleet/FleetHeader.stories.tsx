import { Button } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect } from "storybook/test";
import { busy } from "./Fleet-fixtures";
import { FleetHeader } from "./FleetHeader";

/** The Fleet view's one-line header, shared by both variants. */
const meta = {
  id: "fleet-header",
  title: "Drafts/Fleet/Header",
  component: FleetHeader,
  args: { ...busy, children: <Button variant="default" size="xs">A control</Button> },
} satisfies Meta<typeof FleetHeader>;
export default meta;
type Story = StoryObj<typeof meta>;

export const NeedsYou: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("heading", { name: "Fleet" })).toBeVisible();
    await expect(canvas.getByText("· 5 need you")).toBeVisible();
  },
};

export const AllClear: Story = {
  args: { sessions: busy.sessions.filter(s => s.state === "working") },
  play: async ({ canvas }) => {
    await expect(canvas.getByText("3 machines · 3 projects · 5 sessions")).toBeVisible();
    await expect(canvas.queryByText(/need you/)).toBeNull();
  },
};
