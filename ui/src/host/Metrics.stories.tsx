import { Box, Group } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, screen, userEvent, waitFor } from "storybook/test";
import { fragments } from "../fixtures/fragments";
import { Metrics } from "./Metrics";

/** The header's row of numbers that say whether anything needs you. A
 * count that needs the person takes its tone; a click opens what it is
 * about, or asks Chat. */
const meta = {
  id: "components-header-metrics",
  title: "Components/Header metrics",
  component: Metrics,
  args: { fragment: fragments.header, editing: false, onAsk: fn(), onShow: fn(), onRemove: fn() },
  decorators: [Story => <Group h={56} px="md" wrap="nowrap" style={{ borderBottom: "1px solid var(--driga-border)" }}><Box style={{ flex: 1, minWidth: 0, display: "flex" }}><Story /></Box></Group>],
} satisfies Meta<typeof Metrics>;
export default meta;
type Story = StoryObj<typeof meta>;

export const NeedsYou: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("button", { name: /^Need you\s*3/ })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^Possibly stuck\s*1/ })).toBeVisible();
  },
};
export const WithPrompts: Story = { args: { extra: { waiting: 2 } }, play: async ({ canvas }) => { await expect(canvas.getByRole("button", { name: /^Need you\s*5/ })).toBeVisible(); } };
export const AllClear: Story = { args: { fragment: fragments.header_quiet }, play: async ({ canvas }) => { await expect(canvas.getByRole("button", { name: /^Need you\s*0/ })).toBeVisible(); } };
export const TileMenu: Story = {
  play: async ({ canvas, args }) => {
    await userEvent.click(canvas.getByRole("button", { name: /To review/ }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Show on Home" }));
    await expect(args.onShow).toHaveBeenCalledWith("inbox");
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
  },
};
export const AskChat: Story = {
  play: async ({ canvas, args }) => {
    await userEvent.click(canvas.getByRole("button", { name: /Memory used/ }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Ask Chat about it" }));
    await expect(args.onAsk).toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
  },
};
export const Editing: Story = {
  args: { editing: true },
  play: async ({ canvas, args }) => {
    await userEvent.click(canvas.getByRole("button", { name: "Remove Reclaimable" }));
    await expect(args.onRemove).toHaveBeenCalledWith("reclaim");
  },
};
export const Loading: Story = { args: { fragment: null } };
