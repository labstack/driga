import { spotlight } from "@mantine/spotlight";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, screen, userEvent, waitFor, within } from "storybook/test";
import { answered, awaitingApproval, prompt } from "../fixtures/chat";
import { fleetQuiet } from "../fixtures/fleet";
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

/** A service whose Home is slow to open, so the app is caught loading. */
function slowHome(): Service {
  const service = fakeService();
  return (input, init) => {
    const body = init?.body ? JSON.parse(String(init.body)) as { id?: string } : {};
    if (String(input).includes("/app/api/open") && body.id === "home") return new Promise(() => undefined);
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
    makeService: () => fakeService(),
    docs: { story: { inline: false, iframeHeight: 900 }, description: { component: "What needs you, what finished, what is running away and what is in flight, with Chat beside the board. Drag the line between them to resize Chat." } },
  },
  // Spotlight keeps whether it is open in a module store, and the app writes
  // its page into the URL; each story starts closed, on Home.
  beforeEach: () => { spotlight.close(); history.replaceState(null, "", "/app/"); return () => spotlight.close(); },
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
  parameters: { makeService: () => fakeService({ prompts: [prompt] }) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText("Bash(npm run migrate -- --env staging)")).toBeVisible();
    // This approval belongs to session 11, already counted by its inbox wait.
    await expect(await canvas.findByRole("button", { name: /^Need you\s*3/ })).toBeVisible();
  },
};
export const AllClear: Story = {
  parameters: { makeService: () => fakeService({ open: { home: fragments.home_quiet, header: fragments.header_quiet }, inbox: fragments.inbox_quiet, fleet: fleetQuiet }) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/^Inbox:/)).toBeVisible();
    await expect(await canvas.findByText("Nothing is waiting on you.")).toBeVisible();
    await expect(await canvas.findByRole("button", { name: /^Need you\s*0/ })).toBeVisible();
  },
};
export const ChatConversation: Story = {
  parameters: { makeService: () => fakeService({ chat: answered }) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/uses the most, 1.4 GB/)).toBeVisible();
    await expect(await canvas.findByRole("button", { name: /^Need you\s*3/ })).toBeVisible();
  },
};
export const ChatAwaitingApproval: Story = {
  parameters: { makeService: () => fakeService({ chat: awaitingApproval }) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole("button", { name: `Allow ${awaitingApproval.actions[0].title}` })).toBeVisible();
    await expect(canvas.getByRole("button", { name: `Deny ${awaitingApproval.actions[0].title}` })).toBeVisible();
    await expect(await canvas.findByRole("button", { name: /^Need you\s*3/ })).toBeVisible();
  },
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
export const Fleet: Story = {
  parameters: { docs: { description: { story: "The fleet pulse in the header, g then f, or Search opens the Fleet page in the board's place." } } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole("button", { name: /^Fleet: 3 machines/ })).toBeVisible();
    await userEvent.keyboard("gf");
    await expect(await canvas.findByRole("heading", { name: "Fleet", level: 1 })).toBeVisible();
    await expect(canvas.getByText("3 machines · 4 projects · 9 sessions")).toBeVisible();
  },
};
export const FilterNeedsYou: Story = {
  name: "Filter to what needs you",
  parameters: { docs: { description: { story: "A Need-you tile press narrows the inbox to what needs you; pressing it again shows everything." } } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/To review · 3/)).toBeVisible();
    const tile = await canvas.findByRole("button", { name: /^Need you\s*3/ });
    await userEvent.click(tile);
    await expect(tile).toHaveAttribute("aria-pressed", "true");
    await waitFor(() => expect(canvas.queryByText(/To review · 3/)).toBeNull());
    await userEvent.click(tile);
    await expect(await canvas.findByText(/To review · 3/)).toBeVisible();
    await userEvent.unhover(tile);
    tile.blur();
    await waitFor(() => expect(screen.queryByRole("tooltip")).toBeNull());
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
export const LoadingHome: Story = {
  parameters: { makeService: slowHome },
  play: async ({ canvas }) => {
    // Until Home arrives Chat cannot know whether Home lists the inbox, so it
    // shows none rather than a copy that vanishes when Home does.
    await expect(await canvas.findByText(/2 sessions need you/)).toBeVisible();
    await expect(canvas.queryByRole("article", { name: "Inbox" })).toBeNull();
  },
};
export const ServiceUnreachable: Story = {
  parameters: { makeService: () => fakeService({ down: true }) },
  play: async ({ canvas }) => { await expect(await canvas.findByText(/can't reach its service/)).toBeVisible(); },
};

const desktopCommand = (name: string, arg: unknown) => (window as unknown as { drigaCommand: (name: string, arg: unknown) => void }).drigaCommand(name, arg);

export const NotificationViews: Story = {
 play: async ({ canvas }) => {
  await expect(await canvas.findByRole("heading", { name: "Home", level: 1 })).toBeVisible();
  desktopCommand("view", "fleet");
  await expect(await canvas.findByRole("heading", { name: "Fleet", level: 1 })).toBeVisible();
  desktopCommand("view", "inbox");
  await expect(await canvas.findByRole("heading", { name: "Home", level: 1 })).toBeVisible();
  await expect(await canvas.findByText(/To review · 3/)).toBeVisible();
 },
};

const notificationRequest = fn();
export const NotificationSession: Story = {
 parameters: { makeService: () => fakeService({ onRequest: notificationRequest }) },
 play: async ({ canvas }) => {
  await expect(await canvas.findByRole("heading", { name: "Home", level: 1 })).toBeVisible();
  desktopCommand("openSession", { session_id: 11, machine_id: "studio-id" });
  await waitFor(() => expect(notificationRequest).toHaveBeenCalledWith("/app/action", { action: "jump", session_id: 11, machine: "studio-id" }));
 },
};
