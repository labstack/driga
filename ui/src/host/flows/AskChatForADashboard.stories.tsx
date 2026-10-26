import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, screen, userEvent, waitFor, within } from "storybook/test";
import { answered } from "../../fixtures/chat";
import { App } from "../App";
import { recorded } from "./requests";

const asking = recorded();
const answer = recorded({ chat: answered });

/** The person asks Chat a question; Chat answers in a few lines with a
 * card of live panels that opens in place or full screen. */
const meta = {
  id: "flows-ask-chat-for-a-dashboard",
  title: "Flows/Ask Chat for a dashboard",
  component: App,
  parameters: { layout: "fullscreen", docs: { story: { inline: false, iframeHeight: 900 } } },
} satisfies Meta<typeof App>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Asked: Story = {
  parameters: { makeService: asking.makeService },
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole("button", { name: "What needs me first, and why?" }));
    await waitFor(() => expect(asking.requests.some(r => r.path === "/chat/api/send" && (r.body as { text: string }).text === "What needs me first, and why?")).toBe(true));
  },
};
export const AnsweredWithACard: Story = {
  parameters: { makeService: answer.makeService },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/uses the most, 1.4 GB/)).toBeVisible();
    await expect(await canvas.findByRole("button", { name: /Memory right now/, expanded: true })).toBeVisible();
  },
};
export const FullScreen: Story = {
  parameters: {
    makeService: answer.makeService,
    // Mantine draws a modal's title in a <header>; axe reads it as a second
    // banner, though the page behind the modal is inert.
    a11y: { config: { rules: [{ id: "landmark-no-duplicate-banner", enabled: false }, { id: "landmark-unique", enabled: false }] } },
  },
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole("button", { name: "Full screen" }));
    const dialog = await screen.findByRole("dialog", { name: "Memory right now" });
    await waitFor(() => expect(dialog).toBeVisible());
    await expect(within(dialog).getByRole("heading", { name: "Memory by session" })).toBeVisible();
  },
};
