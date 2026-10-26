import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn } from "storybook/test";
import { fragments } from "../fixtures/fragments";
import { Dashboard } from "./Dashboard";

/** The board that fills the window: panels take the rows their content
 * needs, the last ones give up rows first when the window is short, and
 * empty panels fold into an all-clear strip. Edit mode drags and resizes. */
const meta = {
  id: "components-dashboard",
  title: "Components/Dashboard",
  component: Dashboard,
  args: { fragment: fragments.home, editing: false, act: fn(async () => undefined), onLayout: fn(), onRemove: fn() },
  decorators: [(Story, { parameters }) => <Box h={parameters.boardHeight ?? 760} p="md" style={{ display: "flex", flexDirection: "column" }}><Story /></Box>],
  parameters: { layout: "fullscreen", docs: { story: { inline: false, iframeHeight: 820 } } },
} satisfies Meta<typeof Dashboard>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Busy: Story = { play: async ({ canvas }) => { await expect(canvas.getByRole("heading", { name: "Inbox" })).toBeVisible(); } };
export const AllClear: Story = {
  args: { fragment: fragments.home_quiet },
  play: async ({ canvas }) => { await expect(canvas.getByText(/^Inbox:/)).toBeVisible(); },
};
export const ShortWindow: Story = {
  parameters: { boardHeight: 520 },
  play: async ({ canvas }) => { await expect(canvas.getByRole("heading", { name: "Inbox" })).toBeVisible(); },
};
export const Editing: Story = { args: { editing: true } };
export const EveryPanel: Story = { args: { fragment: fragments.all }, parameters: { boardHeight: 1400, docs: { story: { inline: false, iframeHeight: 1460 } } } };
