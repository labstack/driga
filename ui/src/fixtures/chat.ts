// Fictional Chat conversations and permission prompts for stories.
import type { ChatAction, ChatMessage, ChatState } from "../host/api";
import type { Prompt } from "../panels/types";

let at = Date.parse("2026-03-14T14:50:00Z");
const msg = (id: string, role: string, text: string, extra: Partial<ChatMessage> = {}): ChatMessage =>
  ({ id, role, text, status: "done", created_at: new Date((at += 1000)).toISOString(), ...extra });

const card = (tool: string, input: unknown, more: Record<string, unknown> = {}) => JSON.stringify({ tool, input, ...more });

/** A show_panels card over panels the fixtures hold. */
export const memoryCard = card("show_panels", { dashboard: { name: "Memory right now", panels: [
  { id: "stat_total", title: "Memory used", viz: "stat", source: "activity", measure: "max:mem_used_bytes", total: "max:mem_total_bytes" },
  { id: "bar", title: "Memory by session", viz: "bar", source: "costs", by: "session_id", measure: "sum:mem_bytes" },
  { id: "table", title: "Sessions", viz: "table", source: "sessions" },
] } });

export const textCard = card("show_panels", { dashboard: { name: "Disk by project", panels: [{ id: "text", title: "Notes", viz: "text", content: "Measured on **studio** at 15:00.\n\n| Project | Disk |\n|---|---|\n| atlas | 12 GB |\n| harbor | 8.4 GB |\n| beacon | 2.1 GB |" }] } });

export const savedCard = card("edit_dashboard", { id: "memory", operations: [{ op: "add", panel: { id: "timeseries_one", title: "Swap used on studio", viz: "timeseries", source: "activity" } }, { op: "update", id: "bar", panel: { title: "Memory by session" } }] }, { saved: { id: "memory", version: 4 } });

export const createdCard = card("create_dashboard", { dashboard: { name: "Memory", panels: [{ id: "stat_total", title: "Memory used", viz: "stat", source: "activity" }, { id: "bar", title: "Memory by session", viz: "bar", source: "costs" }] } }, { saved: { id: "memory", version: 1 } });

export const rejectedCard = card("create_dashboard", { dashboard: { name: "Quota", panels: [{ id: "q", title: "Quota", viz: "stat", source: "quota" }] } }, { error: "invalid_spec: panels[0].source: unknown source \"quota\"; sources are sessions, inbox, projects, orphans, cleanup, activity, memory, costs, flow, history, waits, machines, summary" });

export const deletedCard = card("edit_dashboard", { id: "scratch-test", operations: [] }, { saved: { id: "scratch-test", version: 2 } });

const conversation = (messages: ChatMessage[], actions: ChatAction[] = [], active = false): ChatState =>
  ({ conversation: { id: "conversation-1" }, messages, events: [], actions, cursor: 1, reset: true, active, mode: "confirm" });

export const answered = conversation([
  msg("q1", "user", "Which sessions use the most memory right now?"),
  msg("a1", "assistant", "The checkout fix on harbor uses the most, 1.4 GB, with its test run another 380 MB. Studio is at 29 of 32 GB, and swap is 6 GB, so memory is tight.", { html: "<p>The checkout fix on <strong>harbor</strong> uses the most, 1.4 GB, with its test run another 380 MB.</p><ul><li>Studio is at 29 of 32 GB.</li><li>Swap is 6 GB, so memory is tight.</li></ul>" }),
  msg("t1", "tool", "mcp__driga__get_summary"),
  msg("t2", "tool", "mcp__driga__show_panels"),
  msg("p1", "panel", memoryCard),
]);

export const saved = conversation([
  msg("q1", "user", "Add a swap chart to the Memory dashboard."),
  msg("a1", "assistant", "I added Swap used on studio to the Memory dashboard. It is now at version 4."),
  msg("t1", "tool", "mcp__driga__edit_dashboard"),
  msg("p1", "panel", savedCard),
]);

export const rejected = conversation([
  msg("q1", "user", "Make a dashboard of my plan quota."),
  msg("a1", "assistant", "Driga has no quota data yet, so the dashboard was not saved."),
  msg("t1", "tool", "mcp__driga__create_dashboard"),
  msg("p1", "panel", rejectedCard),
]);

export const failed = conversation([
  msg("q1", "user", "What finished while I was away?"),
  msg("a1", "assistant", "", { status: "error" }),
]);

export const forgotten = conversation([
  msg("q0", "user", ""), msg("a0", "assistant", ""),
  msg("q1", "user", "Is anything stuck?"),
  msg("a1", "assistant", "One session looks stuck: the billing migration on build-01 has been busy with no output for 34 minutes."),
]);

const stopAction: ChatAction = { id: "act1", tool: "kill", title: "Stop claude #19 · /home/dev/src/sandbox", input: { session_id: 19 }, directory: "/home/dev/src/sandbox", status: "pending" };

export const awaitingApproval = conversation([
  msg("q1", "user", "Stop the agy session in sandbox."),
  msg("a1", "assistant", "It is session 19, agy, idle with near-zero CPU. I sent the stop request; approve it below.", {}),
  msg("t1", "tool", "mcp__driga__kill"),
  msg("act1", "action", "Stop claude #19 · /home/dev/src/sandbox · pending"),
], [stopAction], true);

export const decided = conversation([
  msg("q1", "user", "Delete the Scratch test dashboard."),
  msg("t1", "tool", "mcp__driga__delete_dashboard"),
  msg("act1", "action", "delete_dashboard scratch-test · done"),
  msg("a1", "assistant", "Deleted Scratch test. Your other dashboards are unchanged."),
], [{ id: "act1", tool: "delete_dashboard", title: "delete_dashboard scratch-test", input: { id: "scratch-test" }, directory: "/home/dev/src/sandbox", status: "done", decided_by: "you", result: "\"{}\"" }]);

export const longBash = "printf '%s\\n' " + "review-this-argument-".repeat(40) + "; rm -rf ./scratch-test";
export const longBashPending = conversation([
  msg("q1", "user", "Clean up the scratch directory."),
  msg("bash1", "action", "Bash · pending"),
], [{ id: "bash1", tool: "Bash", title: "Bash · printf '%s\\n' review-this-argument…", input: { command: longBash }, directory: "/home/dev/src/sandbox", status: "pending" }], true);

export const working = conversation([
  msg("q1", "user", "Summarize what finished on harbor today."),
  msg("t1", "tool", "mcp__driga__list_inbox"),
  msg("a1", "assistant", "", { status: "streaming" }),
], [], true);

export const prompt: Prompt = { id: "prompt-1", session: "Add rate limiting to the upload API", session_id: 11, cwd: "/home/dev/src/atlas", tool: "Bash", input: { command: "npm run migrate -- --env staging" }, created_at: "2026-03-14T14:56:00Z" };
export const editPrompt: Prompt = { id: "prompt-2", session: "Fix the flaky checkout test", session_id: 12, cwd: "/home/dev/src/harbor", tool: "Edit", input: { file_path: "/home/dev/src/harbor/test/fixtures/cart.ts" }, created_at: "2026-03-14T14:58:00Z" };
