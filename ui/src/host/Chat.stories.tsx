import { Box, ScrollArea } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, screen, userEvent, waitFor, within } from "storybook/test";
import { answered, awaitingApproval, decided, failed, forgotten, longBash, longBashPending, rejected, saved, working } from "../fixtures/chat";
import { fragmentOf, panelOf, storyNow } from "../fixtures/fragments";
import { fakeService, type ServiceOptions } from "../fixtures/service";
import { Chat } from "./Chat";
import { HostContext } from "../panels/host";
import { PromptItem, PromptBurstItem } from "../panels/PanelView";
import type { ChatAction, ChatState, CommandCard } from "./api";

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

async function assertSuggestionLabels(root: HTMLElement) {
  await document.fonts.ready;
  const names = ["Is anything stuck?", "What needs me first, and why?", "What finished since I was away, and is it safe to merge?", "Make a dashboard for the project I am working on", "Which processes can I reclaim?"];
  const labels = [...root.querySelectorAll<HTMLElement>(".mantine-Button-label")].filter(label => names.includes(label.textContent ?? ""));
  expect(labels).toHaveLength(4);
  for (const label of labels) {
    const style = getComputedStyle(label);
    expect(Number.parseFloat(style.lineHeight) / Number.parseFloat(style.fontSize), label.textContent!).toBeGreaterThanOrEqual(1.3);
    expect(style.overflowY, label.textContent!).toBe("visible");
    expect(label.scrollHeight, label.textContent!).toBeLessThanOrEqual(label.clientHeight);
  }
}

export const Brief: Story = {
  args: { inbox: { ...panelOf("inbox", "inbox"), listed: false } },
  play: async ({ canvas, canvasElement }) => {
    await expect(await canvas.findByText(/2 sessions need you/)).toBeVisible();
    await expect(canvas.getByRole("button", { name: "What needs me first, and why?" })).toBeVisible();
    await assertSuggestionLabels(canvasElement);
  },
};
export const NothingWaiting: Story = {
  args: { inbox: { ...panelOf("inbox_quiet", "inbox"), listed: true } },
  play: async ({ canvas, canvasElement }) => {
    await expect(await canvas.findByText("Nothing is waiting on you.")).toBeVisible();
    await assertSuggestionLabels(canvasElement);
  },
};
export const WrappedSuggestions: Story = {
  args: NothingWaiting.args,
  decorators: [Story => <Box w={260}><Story /></Box>],
  play: async ({ canvas, canvasElement }) => {
    await expect(await canvas.findByText("Nothing is waiting on you.")).toBeVisible();
    await assertSuggestionLabels(canvasElement);
    const label = canvas.getByRole("button", { name: "What finished since I was away, and is it safe to merge?" }).querySelector<HTMLElement>(".mantine-Button-label")!;
    expect(label.clientHeight).toBeGreaterThan(Number.parseFloat(getComputedStyle(label).lineHeight) * 1.5);
  },
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
    await waitFor(() => expect(canvas.getByLabelText("Current trust mode")).toHaveTextContent(/^Mode: Confirm$/));
    const mode = canvas.getByLabelText("Current trust mode");
    expect(getComputedStyle(mode).whiteSpace).toBe("nowrap");
    expect(mode.getBoundingClientRect().height).toBeLessThan(24);
    const heading = canvas.getByRole("heading", { name: "Chat" }).getBoundingClientRect();
    expect(mode.getBoundingClientRect().left).toBeGreaterThan(heading.right);
    expect(Math.abs(mode.getBoundingClientRect().top - heading.top)).toBeLessThan(4);
    await waitFor(() => expect(canvas.getByRole("button", { name: "Confirm" })).toHaveAttribute("aria-pressed", "true"));
  },
};
export const Auto: Story = {
  parameters: { service: service({ chat: { ...working, mode: "auto" } }) },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByLabelText("Current trust mode")).toHaveTextContent(/^Mode: Auto$/));
    await expect(canvas.getByRole("button", { name: "Auto" })).toHaveAttribute("aria-pressed", "true");
  },
};
export const FullAuto: Story = {
  parameters: { service: service({ chat: { ...working, mode: "full auto", until: new Date(storyNow + 12 * 60_000).toISOString() } }) },
  play: async ({ canvas }) => {
    const mode = await canvas.findByLabelText("Current trust mode");
    await waitFor(() => expect(mode).toHaveTextContent("Mode: Full auto · 12m left"));
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
    await expect(within(dialog).getByText("Full auto runs reads and sandboxed Other commands automatically for 30 minutes. Process and Destructive commands still ask.")).toBeVisible();
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

// Broker presentation fixtures: every identity remains inspectable.
const commandCard: CommandCard = {
  requested: { argv: ["pkill", "-f", "maestro.cli.AppKt mcp"], cwd: "/work/project", timeout_seconds: 120, network: false },
  effective: [], cwd: "/work/project", class: "Process",
  boundary: "Host process snapshot only · no new pattern match · SIGTERM",
  summary: "Stop 65 processes owned by you matching `maestro.cli.AppKt mcp` (about 5 GB)",
  targets: Array.from({ length: 65 }, (_, i) => ({ pid: 100 + i, owner: "UID 501", started_at: "2026-10-29T00:00:00Z", executable: "/usr/bin/java", argv: ["java", "maestro.cli.AppKt mcp"], command_line: "maestro.cli.AppKt mcp", memory_bytes: 77_000_000 })),
  count: 65, memory_bytes: 5_000_000_000, force: false,
};
const readCard: CommandCard = {
  ...commandCard, requested: { ...commandCard.requested, argv: ["cat", "README.md"] },
  effective: [["/bin/cat", "README.md"]], class: "Read", summary: "Run command",
  count: 0, targets: [], memory_bytes: 0,
  boundary: "sandboxed: writes only to /work/project and /scratch, no network",
};
const script = "printf '%s\\n' ready > result.txt";
const scriptCard: CommandCard = { ...readCard, class: "Other", requested: { ...readCard.requested, argv: undefined, script }, effective: [["/bin/sh", "-c", script]] };
const commandState = (command = commandCard, more: Partial<ChatAction> = {}): ChatState => ({
  ...awaitingApproval,
  messages: [
    { id: "q1", role: "user", text: "Run this local command.", status: "done", created_at: new Date(storyNow - 1000).toISOString() },
    { id: "cmd1", role: "action", text: command.summary, status: "pending", created_at: new Date(storyNow).toISOString() },
  ],
  actions: [{ id: "cmd1", tool: "mcp__driga__run_command", title: command.summary, input: command.requested, directory: command.cwd, status: "pending", command, ...more }],
});
export const Command: Story = { parameters: { service: service({ chat: commandState(readCard) }) } };
export const Process65: Story = {
  parameters: { service: service({ chat: commandState() }) },
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole("button", { name: "Inspect 65 process targets" }));
    await expect(await canvas.findByText(/PID 164/)).toBeInTheDocument();
  },
};
const outcomesCard: CommandCard = { ...commandCard, stopped: commandCard.targets.slice(0, 62), vanished: [commandCard.targets[62]], skipped: [{ ...commandCard.targets[63], reason: "Identity changed; not signalled" }], survivors: [commandCard.targets[64]] };
export const ProcessOutcomes: Story = { parameters: { service: service({ chat: commandState(outcomesCard, { status: "done", decided_by: "you", result: "Stopped 62, vanished 1, skipped 1, survivors 1" }) }) } };
const forceCard: CommandCard = { ...commandCard, force: true, targets: [commandCard.targets[64]], count: 1, memory_bytes: commandCard.targets[64].memory_bytes, summary: "Force stop 1 surviving processes owned by you", boundary: "Host process snapshot only · no new pattern match · SIGKILL · second approval" };
const forceState = commandState(forceCard);
forceState.messages.splice(1, 0, { id: "term", role: "action", text: commandCard.summary, status: "done", created_at: new Date(storyNow - 500).toISOString() });
forceState.actions.unshift({ ...commandState().actions[0], id: "term", status: "done", decided_by: "you", result: "Stopped 64, vanished 0, skipped 0, survivors 1", command: { ...commandCard, stopped: commandCard.targets.slice(0, 64), survivors: [commandCard.targets[64]] } });
export const ForceSecondCard: Story = { parameters: { service: service({ chat: forceState }) } };
export const Script: Story = { parameters: { service: service({ chat: commandState(scriptCard) }) } };
export const UnavailableSandbox: Story = { parameters: { service: service({ chat: commandState({ ...scriptCard, error: "Running commands needs a sandbox, which Windows does not have yet." }) }) } };
export const GrantOffer: Story = {
  parameters: { service: service({ chat: commandState(readCard, { grant_eligible: true, grant_pattern: 'Read · ["cat","README.md"] · in /work/project', grant_expires_at: "2026-10-29T00:30:00Z" }) }) },
};
export const GrantAttributionAndRevoke: Story = {
  parameters: { service: service({ chat: commandState(readCard, { status: "done", decided_by: "allowed by your grant", grant_id: "grant-1" }), onRequest: (path, body) => requests.push({ path, body }) }) },
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole("button", { name: "Revoke grant" }));
    await expect(await canvas.findByRole("button", { name: "Grant revoked" })).toBeDisabled();
  },
};
export const SessionGrantOffer: Story = {
  render: () => <HostContext.Provider value={{ answerSimilar: fn(async () => undefined) }}><PromptItem prompt={{ id: "hook-1", session: "agent-session", cwd: "/work/project", tool: "Bash", input: { command: "git worktree remove /work/project/old" }, created_at: "2026-10-29T00:00:00Z", grant_eligible: true, grant_pattern: 'Other · ["git","worktree","remove","<path under /work/project>"]', grant_expires_at: "2026-10-29T00:30:00Z" }} answer={fn(async () => undefined)} /></HostContext.Provider>,
};
const promptBurst = {
  id: "burst-1", session: "agent-session", cwd: "/work/project", opened_at: "2026-10-29T00:00:00Z",
  members: Array.from({ length: 9 }, (_, i) => ({ id: `p${i + 1}`, input_hash: `hash-${i + 1}`, prompt: { id: `p${i + 1}`, session: "agent-session", cwd: "/work/project", tool: "Bash", input: { command: `git worktree remove /work/project/branch-${i + 1}`, description: `Remove reviewed worktree ${i + 1}` }, created_at: "2026-10-29T00:00:00Z" } })),
};
export const PromptBurstNine: Story = {
  render: () => <ScrollArea h="100%" viewportProps={{ tabIndex: 0, "aria-label": "Prompt burst" }}><PromptBurstItem burst={promptBurst} answer={fn(async () => undefined)} decide={fn(async () => undefined)} /></ScrollArea>,
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole("button", { name: "Allow all 9" })).toBeInTheDocument();
    await expect(canvas.getAllByRole("button", { name: /Deny/ })).toHaveLength(9);
  },
};
