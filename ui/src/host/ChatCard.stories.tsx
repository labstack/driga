import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, screen, userEvent, waitFor, within } from "storybook/test";
import { createdCard, deletedCard, memoryCard, rejectedCard, savedCard, textCard } from "../fixtures/chat";
import { fragmentOf } from "../fixtures/fragments";
import { fakeService } from "../fixtures/service";
import { ChatCard } from "./ChatCard";

const memory = fragmentOf({ name: "Memory", panels: [
  { id: "stat_total", title: "Memory used", viz: "stat", source: "activity" },
  { id: "bar", title: "Memory by session", viz: "bar", source: "costs" },
  { id: "timeseries_one", title: "Swap used on studio", viz: "timeseries", source: "activity" },
] });

/** What a panel tool showed or changed, in the Chat column: a few lines
 * that expand in place, stacked to fit, or open full screen. A write's
 * card says what it saved, or why it could not. */
const meta = {
  id: "components-chat-card",
  title: "Components/Chat card",
  component: ChatCard,
  args: { text: memoryCard, open: false, onOpen: fn() },
  decorators: [Story => <Box w={368}><Story /></Box>],
  globals: { viewport: { value: "chat", isRotated: false } },
  parameters: { service: fakeService({ open: { memory: { ...memory, version: 4 } } }) },
} satisfies Meta<typeof ChatCard>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Collapsed: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByText("Memory right now")).toBeVisible();
    await expect(canvas.getByText(/29\.\d GB of 32 GB|of 32 GB/)).toBeVisible();
  },
};
export const Expanded: Story = {
  args: { open: true },
  play: async ({ canvas }) => { await expect(await canvas.findByRole("heading", { name: "Memory by session" })).toBeVisible(); },
};
export const ExpandAndCollapse: Story = {
  play: async ({ canvas }) => {
    const toggle = await canvas.findByRole("button", { name: /Memory right now/ });
    await userEvent.click(toggle);
    await expect(toggle).toHaveAttribute("aria-expanded", "true");
    await expect(await canvas.findByRole("heading", { name: "Memory by session" })).toBeVisible();
    await userEvent.click(toggle);
    await expect(toggle).toHaveAttribute("aria-expanded", "false");
  },
};
export const FullScreen: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole("button", { name: "Full screen" }));
    const dialog = await screen.findByRole("dialog", { name: "Memory right now" });
    await waitFor(() => expect(dialog).toBeVisible());
    await expect(within(dialog).getByRole("heading", { name: "Sessions" })).toBeVisible();
  },
};
export const Saved: Story = {
  args: { text: savedCard },
  play: async ({ canvas, args }) => {
    await expect(await canvas.findByText("Saved Memory")).toBeVisible();
    await expect(canvas.getByText(/version 4/)).toBeVisible();
    await expect(canvas.getByText("Added Swap used on studio")).toBeVisible();
    await userEvent.click(canvas.getByRole("button", { name: "Open dashboard" }));
    await expect(args.onOpen).toHaveBeenCalledWith("memory");
  },
};
export const Created: Story = { args: { text: createdCard }, play: async ({ canvas }) => { await expect(await canvas.findByText("Created with 2 panels")).toBeVisible(); } };
export const DraftRejected: Story = { args: { text: rejectedCard }, play: async ({ canvas }) => { await expect(canvas.getByText("Draft rejected: Quota")).toBeVisible(); } };
export const DeletedSince: Story = { args: { text: deletedCard }, play: async ({ canvas }) => { await expect(await canvas.findByText(/was deleted since/)).toBeVisible(); } };
export const TextTable: Story = { args: { text: textCard, open: true }, play: async ({ canvas }) => { await expect(await canvas.findByRole("cell", { name: "atlas" })).toBeVisible(); } };
export const Unreadable: Story = { args: { text: "{not json" }, play: async ({ canvas }) => { await expect(canvas.getByText("This card could not be read.")).toBeVisible(); } };
