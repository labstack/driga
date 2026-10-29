import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn } from "storybook/test";
import { fragments } from "../fixtures/fragments";
import { HeaderEditor } from "./HeaderEditor";

const meta = {
  id: "components-header-editor",
  title: "Components/Header editor",
  component: HeaderEditor,
  args: { panels: fragments.header.dashboard.panels, busy: false, onAdd: fn(), onReset: fn(), onAsk: fn() },
} satisfies Meta<typeof HeaderEditor>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Controls: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("combobox", { name: "Add widget" })).toBeVisible();
    await expect(canvas.getByRole("button", { name: "Header options" })).toBeVisible();
  },
};
