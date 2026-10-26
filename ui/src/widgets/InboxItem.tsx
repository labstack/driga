// InboxItem is one thing that needs the person, in three lines at most: a
// rule on the left in the kind's color, the agent's chip, the title with
// the kind in words, the reason in an inset mono box (the decision input:
// the command a permission asks for, the question, the last tool line of a
// stuck session), where it runs, how long ago and how long it has waited.
// Its actions wait until the row is hovered, focused or selected, with
// their keys in the tooltips: a, o, x.
import { ActionIcon, Box, Button, Group, Kbd, Stack, Text, Tooltip, VisuallyHidden } from "@mantine/core";
import { Desktop, X } from "@phosphor-icons/react";
import type { KeyboardEvent, MouseEvent } from "react";
import { duration, exact, plural, relative } from "../panels/format";
import type { Tone } from "../theme/tokens";
import { AgentChip } from "./AgentChip";
import "./widgets.css";

/** What an item asks: a permission or a question an agent waits on, any
 * other wait, a finished turn to review, a session that looks stuck, or a
 * machine warning. */
export type InboxKind = "permission" | "question" | "waiting" | "finished" | "stuck" | "machine";

export type InboxEntry = {
  id: string;
  kind: InboxKind;
  title: string;
  /** The adapter name: claude, codex, gemini, copilot, agy. */
  agent?: string;
  /** The decision input: a tool call, a question, the last tool line, a machine's numbers. */
  reason?: string;
  project?: string;
  branch?: string;
  machine?: string;
  /** When the item arose. */
  at: string;
  /** Since when it has waited on the person, or been quiet. */
  since?: string;
  turns?: number;
  /** How long a session is normally quiet, in ms; a stuck one says it. */
  quietExpected?: number;
  /** A finished session's review facts, and the checkout they were read from. */
  review?: { files: number; uncommitted: number; unpushed: number; checkout?: string };
};

export type InboxItemProps = {
  entry: InboxEntry;
  selected?: boolean;
  /** The kind's main action: approve, answer, reply, mark reviewed, reclaim. */
  onApprove?: (entry: InboxEntry) => void;
  /** A permission's other answer. */
  onDeny?: (entry: InboxEntry) => void;
  onOpen?: (entry: InboxEntry) => void;
  onDismiss?: (entry: InboxEntry) => void;
  /** Approve and Deny arm after a moment, so a click meant for the row that
   * was there before never answers the one that moved into place. */
  armed?: boolean;
};

const kindTone: Record<InboxKind, Tone> = { permission: "warning", question: "warning", waiting: "warning", stuck: "danger", finished: "info", machine: "warning" };

const primaryLabel: Record<InboxKind, string | null> = { permission: "Approve", question: "Answer", waiting: "Reply", finished: "Mark reviewed", stuck: null, machine: "Reclaim" };

/** waited reads a wait to the second under an hour: 2m 14s. */
export function waited(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m ${s % 60}s`;
  return duration(ms);
}

function kindWord(e: InboxEntry, now: number): string {
  switch (e.kind) {
    case "permission": return "needs approval";
    case "question": return "asks a question";
    case "waiting": return "waits on you";
    case "finished": return e.turns && e.turns > 1 ? `finished · ${e.turns} turns` : "finished";
    case "stuck": return `quiet ${duration(now - Date.parse(e.since ?? e.at))}${e.quietExpected ? `, expected ${duration(e.quietExpected)}` : ""}`;
    case "machine": return "machine";
  }
}

export function InboxItem({ entry: e, selected, onApprove, onDeny, onOpen, onDismiss, armed = true }: InboxItemProps) {
  const now = Date.now();
  const tone = kindTone[e.kind];
  const primary = primaryLabel[e.kind];
  const waitedFor = e.since ? waited(now - Date.parse(e.since)) : null;
  const where = [e.project, e.branch, e.machine].filter(Boolean).join(" · ");
  const open = () => onOpen?.(e);
  const onKey = (ev: KeyboardEvent<HTMLDivElement>) => {
    if (ev.target !== ev.currentTarget) return;
    if (ev.key === "Enter" || ev.key === "o") { ev.preventDefault(); open(); }
    else if (ev.key === "a" && primary && onApprove && armed) { ev.preventDefault(); onApprove(e); }
    else if (ev.key === "x" && onDismiss) { ev.preventDefault(); onDismiss(e); }
  };
  const onClick = (ev: MouseEvent<HTMLDivElement>) => { if (!(ev.target as HTMLElement).closest("button, a")) open(); };
  const hint = (text: string, key: string) => <Group gap={6} wrap="nowrap">{text}<Kbd size="xs">{key}</Kbd></Group>;
  // Every action names its item too, so a list of them stays telling apart.
  const named = (text: string) => <>{text}<VisuallyHidden> · {e.title}</VisuallyHidden></>;
  return <Box className="w-row" data-kind={e.kind} data-clickable={onOpen ? "" : undefined} data-selected={selected ? "true" : undefined} tabIndex={onOpen ? 0 : undefined} onClick={onOpen ? onClick : undefined} onKeyDown={onOpen ? onKey : undefined}
    px="sm" py={8} style={{ borderLeft: `3px solid var(--mantine-color-${tone}-filled)`, borderRadius: "var(--mantine-radius-sm)", background: "var(--driga-raised)" }}>
    {/* The content keeps 220 px; the time and the actions wrap under it when the row is narrower. */}
    <Group gap="sm" wrap="wrap" align="flex-start">
      <Box pt={2} style={{ flex: "none" }}>
        {e.kind === "machine" || !e.agent ? <Box role="img" aria-label={e.machine ? `machine ${e.machine}` : "machine"} style={{ width: 20, height: 20, borderRadius: "50%", background: "var(--driga-inset)", display: "grid", placeItems: "center", color: "var(--driga-muted)" }}><Desktop size={12} /></Box> : <AgentChip agent={e.agent} waiting={e.kind === "permission" || e.kind === "question" || e.kind === "waiting"} />}
      </Box>
      <Stack gap={4} style={{ flex: "1 1 220px", minWidth: 0 }}>
        <Group gap={6} wrap="nowrap" align="baseline" style={{ minWidth: 0 }}>
          <Text size="md" fw={500} lh={1.2} truncate title={e.title} style={{ minWidth: 0, flex: "0 1 auto" }}>{e.title}</Text>
          <Text size="xs" c="dimmed" lh={1.2} style={{ whiteSpace: "nowrap", flex: "none" }}>{kindWord(e, now)}</Text>
        </Group>
        {e.reason && <Text size="xs" ff={e.kind === "question" || e.kind === "machine" ? undefined : "monospace"} className="w-inset" lh={1.4} truncate title={e.reason} style={{ display: "block", maxWidth: "100%", alignSelf: "flex-start" }}>{e.reason}</Text>}
        {where && <Text size="xs" c="dimmed" ff="monospace" lh={1.2} truncate title={where}>{where}</Text>}
        {e.review && <Group gap={6} wrap="wrap">
          {e.review.files > 0 && <Text size="xs" c="dimmed" lh={1.2} style={{ whiteSpace: "nowrap" }}>{plural(e.review.files, "file")}</Text>}
          {!e.review.files && !e.review.uncommitted && !e.review.unpushed && (e.review.checkout
            ? <Tooltip label={<Text size="xs" ff="monospace">{e.review.checkout}</Text>}><span className="w-chip" data-tone="neutral">clean</span></Tooltip>
            : <Text size="xs" c="dimmed" lh={1.2} style={{ whiteSpace: "nowrap" }}>no checkout</Text>)}
          {e.review.uncommitted > 0 && <span className="w-chip" data-tone="warning"><Box aria-hidden style={{ width: 5, height: 5, borderRadius: "50%", background: "var(--mantine-color-warning-filled)" }} />{e.review.uncommitted} uncommitted</span>}
          {e.review.unpushed > 0 && <span className="w-chip" data-tone="neutral">{e.review.unpushed} unpushed</span>}
        </Group>}
      </Stack>
      <Stack gap={2} align="flex-end" w={96} style={{ flex: "none" }}>
        <Text size="xs" ff="monospace" lh={1.2} title={exact(e.at)} style={{ whiteSpace: "nowrap" }}>{relative(e.at, now)}</Text>
        {waitedFor && <Group gap={4} wrap="nowrap">
          <Box aria-hidden className={e.kind === "permission" || e.kind === "question" || e.kind === "waiting" ? "w-pulse" : undefined} style={{ width: 6, height: 6, borderRadius: "50%", background: `var(--mantine-color-${tone}-filled)`, flex: "none" }} />
          <Text size="xs" ff="monospace" lh={1.2} style={{ whiteSpace: "nowrap" }}>{e.kind === "stuck" ? "quiet" : "waiting"} {waitedFor}</Text>
        </Group>}
      </Stack>
      {(onApprove || onDeny || onOpen || onDismiss) && <Group gap={4} wrap="nowrap" className="w-actions" style={{ flex: "none" }}>
        {primary && onApprove && <Tooltip label={hint(primary, "a")}><Button size="compact-xs" variant="light" disabled={!armed} onClick={() => onApprove(e)}>{named(primary)}</Button></Tooltip>}
        {e.kind === "permission" && onDeny && <Button size="compact-xs" variant="default" disabled={!armed} onClick={() => onDeny(e)}>{named("Deny")}</Button>}
        {onOpen && <Tooltip label={hint("Open", "o")}><Button size="compact-xs" variant="subtle" color="gray" onClick={open}>{named("Open")}</Button></Tooltip>}
        {onDismiss && <Tooltip label={hint("Dismiss", "x")}><ActionIcon size="sm" variant="subtle" color="gray" aria-label={`Dismiss ${e.title}`} onClick={() => onDismiss(e)}><X size={14} /></ActionIcon></Tooltip>}
      </Group>}
    </Group>
  </Box>;
}
