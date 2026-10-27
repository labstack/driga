import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, screen, userEvent, waitFor, within } from "storybook/test";
import { answered, awaitingApproval, decided, failed, forgotten, longBash, longBashPending, rejected, saved, working } from "../fixtures/chat";
import { fragmentOf, panelOf, storyNow } from "../fixtures/fragments";
import { fakeService, type ServiceOptions } from "../fixtures/service";
import { Chat } from "./Chat";

const memory = fragmentOf({ name: "Memory", panels: [{ id: "stat_total", title: "Memory used", viz: "stat" }, { id: "bar", title: "Memory by session", viz: "bar" }, { id: "timeseries_one", title: "Swap used on studio", viz: "timeseries" }] });
const service = (o: ServiceOptions = {}) => fakeService({ open: { memory: { ...memory, version: 4 } }, ...o });
const inbox = { ...panelOf("inbox", "inbox"), listed: true };
let requests: { path: string; body: unknown }[] = [];

/** Chat beside the board: a brief of what needs you when there is no
 * conversation, then the conversation, with its tool steps, cards and
 * approval cards, and the answer last in each turn. */
const meta = {
  id: "components-chat",
  title: "Components/Chat",
  component: Chat,
  args: { inbox, act: fn(async () => undefined), ask: null, onOpen: fn(), onClose: fn() },
  decorators: [Story => <Box w={400} h={820} style={{ display: "flex", flexDirection: "column", background: "var(--driga-inset)" }}><Story /></Box>],
  globals: { viewport: { value: "chat", isRotated: false } },
  parameters: { service: service(), docs: { story: { inline: false, iframeHeight: 860 } } },
  beforeEach: () => { requests = []; },
} satisfies Meta<typeof Chat>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Brief: Story = {
  args: { inbox: { ...panelOf("inbox", "inbox"), listed: false } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/2 sessions need you/)).toBeVisible();
    await expect(canvas.getByRole("button", { name: "What needs me first, and why?" })).toBeVisible();
  },
};
export const NothingWaiting: Story = {
  args: { inbox: { ...panelOf("inbox_quiet", "inbox"), listed: true } },
  play: async ({ canvas }) => { await expect(await canvas.findByText("Nothing is waiting on you.")).toBeVisible(); },
};
export const Answered: Story = {
  parameters: { service: service({ chat: answered }) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/uses the most, 1.4 GB/)).toBeVisible();
    await expect(await canvas.findByText("Memory right now")).toBeVisible();
  },
};
export const SavedADashboard: Story = { parameters: { service: service({ chat: saved }) }, play: async ({ canvas }) => { await expect(await canvas.findByText("Saved Memory")).toBeVisible(); } };
export const DraftRejected: Story = { parameters: { service: service({ chat: rejected }) }, play: async ({ canvas }) => { await expect(await canvas.findByText("Draft rejected: Quota")).toBeVisible(); } };
export const AwaitingApproval: Story = {
  parameters: { service: service({ chat: awaitingApproval, onRequest: (path, body) => requests.push({ path, body }) }) },
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole("button", { name: `Deny ${awaitingApproval.actions[0].title}` }));
    await waitFor(() => expect(requests).toContainEqual({ path: "/chat/api/decide", body: { id: "conversation-1", action: "act1", allow: false } }));
  },
};
export const Decided: Story = {
  parameters: { service: service({ chat: decided }) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/delete_dashboard scratch-test/)).toBeVisible();
    await expect(canvas.queryByText('"{}"')).toBeNull();
    await expect(canvas.getByText("Decided by you")).toBeVisible();
  },
};
export const Confirm: Story = {
  parameters: { service: service({ chat: { ...working, mode: "confirm" } }) },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByLabelText("Current trust mode")).toHaveTextContent(/^Confirm$/));
    await waitFor(() => expect(canvas.getByRole("button", { name: "Confirm" })).toHaveAttribute("aria-pressed", "true"));
  },
};
export const Auto: Story = {
  parameters: { service: service({ chat: { ...working, mode: "auto" } }) },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByLabelText("Current trust mode")).toHaveTextContent(/^Auto$/));
    await expect(canvas.getByRole("button", { name: "Auto" })).toHaveAttribute("aria-pressed", "true");
  },
};
export const FullAuto: Story = {
  parameters: { service: service({ chat: { ...working, mode: "full auto", until: new Date(storyNow + 12 * 60_000).toISOString() } }) },
  play: async ({ canvas }) => {
    const mode = await canvas.findByLabelText("Current trust mode");
    await waitFor(() => expect(mode).toHaveTextContent("Full auto · 12m left"));
    await expect(mode.style.color).toBe("var(--mantine-color-danger-text)");
  },
};
export const FullAutoConfirmation: Story = {
  parameters: { service: service({ chat: { ...working, mode: "auto" }, onRequest: (path, body) => requests.push({ path, body }) }) },
  play: async ({ canvas }) => {
    const fullAuto = await canvas.findByRole("button", { name: "Full auto" });
    await waitFor(() => expect(fullAuto).toBeEnabled());
    await userEvent.click(fullAuto);
    const dialog = await screen.findByRole("dialog", { name: "Enable Full auto?" });
    await expect(within(dialog).getByText("Full auto runs every action without asking for 30 minutes")).toBeVisible();
    await expect(requests.some(r => r.path === "/chat/api/mode")).toBe(false);
    await userEvent.click(within(dialog).getByRole("button", { name: "Enable Full auto" }));
    await waitFor(() => expect(requests).toContainEqual({ path: "/chat/api/mode", body: { id: "conversation-1", mode: "full auto" } }));
  },
};
export const LongBashPending: Story = {
  parameters: { service: service({ chat: longBashPending }) },
  play: async ({ canvas }) => {
    const input = await canvas.findByText(/rm -rf \.\/scratch-test/);
    await expect(JSON.parse(input.textContent ?? "")).toEqual({ command: longBash });
    await expect(input).toHaveStyle({ whiteSpace: "pre-wrap", overflowWrap: "anywhere" });
    await expect(canvas.getByText("in /home/dev/src/sandbox")).toBeVisible();
    await expect(canvas.getByRole("button", { name: `Allow ${longBashPending.actions[0].title}` })).toBeVisible();
  },
};
export const Working: Story = { parameters: { service: service({ chat: working }) }, play: async ({ canvas }) => { await expect(await canvas.findByText("Working…")).toBeVisible(); } };
export const ReplyFailed: Story = { parameters: { service: service({ chat: failed }) }, play: async ({ canvas }) => { await expect(await canvas.findByText(/This reply failed/)).toBeVisible(); } };
export const EarlierMessagesNotKept: Story = { parameters: { service: service({ chat: forgotten }) }, play: async ({ canvas }) => { await expect(await canvas.findByText(/Earlier messages aren't kept/)).toBeVisible(); } };
export const Ask: Story = {
  parameters: { service: service({ onRequest: (path, body) => requests.push({ path, body }) }) },
  play: async ({ canvas }) => {
    await userEvent.type(canvas.getByRole("textbox", { name: "Ask or act" }), "Is anything stuck?{Enter}");
    await waitFor(() => expect(requests.some(r => r.path === "/chat/api/send" && (r.body as { text: string }).text === "Is anything stuck?")).toBe(true));
  },
};
export const ServiceUnreachable: Story = {
  parameters: { service: fakeService({ down: true }) },
  play: async ({ canvas }) => {
    await userEvent.type(canvas.getByRole("textbox", { name: "Ask or act" }), "hello{Enter}");
    await expect(await canvas.findByText("Failed to fetch")).toBeVisible();
    await expect(canvas.getByRole("textbox", { name: "Ask or act" })).toHaveValue("hello");
  },
};
