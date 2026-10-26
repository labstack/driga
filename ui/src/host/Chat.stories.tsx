import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, waitFor } from "storybook/test";
import { answered, awaitingApproval, decided, failed, forgotten, rejected, saved, working } from "../fixtures/chat";
import { fragmentOf, panelOf } from "../fixtures/fragments";
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
