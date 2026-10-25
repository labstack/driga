import { spotlight } from "@mantine/spotlight";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, screen, userEvent, waitFor, within } from "storybook/test";
import { answered, awaitingApproval, prompt } from "../fixtures/chat";
import { fragments } from "../fixtures/fragments";
import { fakeService, type Service } from "../fixtures/service";
import { App } from "./App";

/** A service that answers once, then stops, as when the Driga service
 * quits while the app is open. */
function failsAfterFirstLoad(): Service {
  const service = fakeService();
  let opened = 0;
  return (input, init) => {
    if (String(input).includes("/app/api/open") && ++opened > 2) return Promise.reject(new TypeError("Failed to fetch"));
    return service(input, init);
  };
}

/** The app: the header's numbers, Home's board and Chat beside it, over
 * the fictional fleet, served by a fake service. */
const meta = {
  id: "pages-home",
  title: "Pages/Home",
  component: App,
  parameters: {
    layout: "fullscreen",
    service: fakeService(),
    docs: { story: { inline: false, iframeHeight: 900 }, description: { component: "What needs you, what finished, what is running away and what is in flight, with Chat beside the board. Drag the line between them to resize Chat." } },
  },
  // Spotlight keeps whether it is open in a module store; each story starts closed.
  beforeEach: () => { spotlight.close(); return () => spotlight.close(); },
} satisfies Meta<typeof App>;
export default meta;
type Story = StoryObj<typeof meta>;

export const NeedsYou: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole("heading", { name: "Home", level: 1 })).toBeVisible();
    await expect(await canvas.findByRole("button", { name: /^Need you\s*3/ })).toBeVisible();
    await expect(await canvas.findByText(/Needs you · 2/)).toBeVisible();
  },
};
export const PermissionPrompt: Story = {
  parameters: { service: fakeService({ prompts: [prompt] }) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText("npm run migrate -- --env staging")).toBeVisible();
    await expect(await canvas.findByRole("button", { name: /^Need you\s*4/ })).toBeVisible();
  },
};
export const AllClear: Story = {
  parameters: { service: fakeService({ open: { home: fragments.home_quiet, header: fragments.header_quiet }, inbox: fragments.inbox_quiet }) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/^Inbox:/)).toBeVisible();
    await expect(await canvas.findByText("Nothing is waiting on you.")).toBeVisible();
  },
};
export const ChatConversation: Story = {
  parameters: { service: fakeService({ chat: answered }) },
  play: async ({ canvas }) => { await expect(await canvas.findByText(/uses the most, 1.4 GB/)).toBeVisible(); },
};
export const ChatAwaitingApproval: Story = {
  parameters: { service: fakeService({ chat: awaitingApproval }) },
  play: async ({ canvas }) => { await expect(await canvas.findByRole("button", { name: "Allow" })).toBeVisible(); },
};
export const ChatHidden: Story = {
  beforeEach: () => { localStorage.setItem("driga-chat-open", "false"); },
  play: async ({ canvas }) => {
    const toggle = await canvas.findByRole("button", { name: "Chat" });
    await expect(toggle).toHaveAttribute("aria-pressed", "false");
    await userEvent.click(toggle);
    await waitFor(() => expect(toggle).toHaveAttribute("aria-pressed", "true"));
  },
};
export const Editing: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole("button", { name: "Edit" }));
    await expect(await canvas.findByText(/Drag panels by their title/)).toBeVisible();
    await expect(canvas.getByRole("button", { name: "Reset Home" })).toBeVisible();
  },
};
export const Search: Story = {
  // Mantine draws the search dialog's title in a <header> inside the modal;
  // axe reads it as a second banner, though the modal makes the page
  // behind it inert, so assistive technology meets one banner at a time.
  parameters: { a11y: { config: { rules: [{ id: "landmark-no-duplicate-banner", enabled: false }, { id: "landmark-unique", enabled: false }] } } },
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole("button", { name: "Search" }));
    const dialog = await screen.findByRole("dialog", { name: "Search sessions and dashboards" });
    await expect(await within(dialog).findByRole("button", { name: /Fix the flaky checkout test/ })).toBeInTheDocument();
  },
};
export const Stale: Story = {
  parameters: { makeService: failsAfterFirstLoad },
  play: async ({ canvas }) => { await expect(await canvas.findByText(/Not updated since/, {}, { timeout: 15_000 })).toBeVisible(); },
};
export const ServiceUnreachable: Story = {
  parameters: { service: fakeService({ down: true }) },
  play: async ({ canvas }) => { await expect(await canvas.findByText(/can't reach its service/)).toBeVisible(); },
};
