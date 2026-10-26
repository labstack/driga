// InboxItem is one thing that needs the person, in three lines at most: a
// rule on the left in the kind's color, the agent's chip, the title with
// the kind in words, the reason in an inset mono box (the decision input:
// the command a permission asks for, the question, the last tool line of a
// stuck session), where it runs, how long ago and how long it has waited.
// Its actions wait until the row is hovered, focused or selected, with
// their keys in the tooltips: a, o, x.
import { ActionIcon, Badge, Box, Button, Group, Kbd, Stack, Text, Tooltip } from "@mantine/core";
import { Desktop, X } from "@phosphor-icons/react";
import type { KeyboardEvent, MouseEvent } from "react";
import { duration, exact, relative } from "../../panels/format";
import type { Tone } from "../../theme/tokens";
import { AgentChip } from "./AgentChip";
import type { Agent } from "./types";
import "./widgets.css";

export type InboxKind = "permission" | "question" | "finished" | "stuck" | "machine";

export type InboxEntry = {
  id: string;
  kind: InboxKind;
  title: string;
  agent?: Agent;
  /** The decision input: a tool call, a question, the last tool line, a machine's numbers. */
  reason?: string;
  project?: string;
  branch?: string;
  machine: string;
  /** When the item arose. */
  at: string;
  /** Since when it has waited on the person, or been quiet. */
  since?: string;
  turns?: number;
  /** How long a session is normally quiet, in ms; a stuck one says it. */
  quietExpected?: number;
  /** A finished session's review facts. */
  review?: { files: number; uncommitted: number; unpushed: number };
};

export type InboxItemProps = {
  entry: InboxEntry;
  selected?: boolean;
  /** The kind's main action: approve, answer, mark reviewed, reclaim. */
  onApprove?: (entry: InboxEntry) => void;
  onOpen?: (entry: InboxEntry) => void;
  onDismiss?: (entry: InboxEntry) => void;
};

const kindTone: Record<InboxKind, Tone> = { permission: "warning", question: "warning", stuck: "danger", finished: "info", machine: "warning" };

const primaryLabel: Record<InboxKind, string | null> = { permission: "Approve", question: "Answer", finished: "Mark reviewed", stuck: null, machine: "Reclaim" };

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
    case "finished": return e.turns ? `finished · ${e.turns} turns` : "finished";
    case "stuck": return `quiet ${duration(now - Date.parse(e.since ?? e.at))}${e.quietExpected ? `, expected ${duration(e.quietExpected)}` : ""}`;
    case "machine": return "machine";
  }
}

function plural(n: number, word: string): string { return `${n} ${word}${n === 1 ? "" : "s"}`; }

export function InboxItem({ entry: e, selected, onApprove, onOpen, onDismiss }: InboxItemProps) {
  const now = Date.now();
  const tone = kindTone[e.kind];
  const primary = primaryLabel[e.kind];
  const waitedFor = e.since ? waited(now - Date.parse(e.since)) : null;
  const where = [e.project, e.branch, e.machine].filter(Boolean).join(" · ");
  const open = () => onOpen?.(e);
  const onKey = (ev: KeyboardEvent<HTMLDivElement>) => {
    if (ev.target !== ev.currentTarget) return;
    if (ev.key === "Enter" || ev.key === "o") { ev.preventDefault(); open(); }
    else if (ev.key === "a" && primary && onApprove) { ev.preventDefault(); onApprove(e); }
    else if (ev.key === "x" && onDismiss) { ev.preventDefault(); onDismiss(e); }
  };
  const onClick = (ev: MouseEvent<HTMLDivElement>) => { if (!(ev.target as HTMLElement).closest("button, a")) open(); };
  const hint = (text: string, key: string) => <Group gap={6} wrap="nowrap">{text}<Kbd size="xs">{key}</Kbd></Group>;
  return <Box className="w-row" data-kind={e.kind} data-clickable={onOpen ? "" : undefined} data-selected={selected ? "true" : undefined} tabIndex={onOpen ? 0 : undefined} onClick={onOpen ? onClick : undefined} onKeyDown={onOpen ? onKey : undefined}
    px="sm" py={8} style={{ borderLeft: `3px solid var(--mantine-color-${tone}-filled)`, borderRadius: "var(--mantine-radius-sm)", background: "var(--driga-raised)" }}>
    <Group gap="sm" wrap="nowrap" align="flex-start">
      <Box pt={2} style={{ flex: "none" }}>
        {e.agent ? <AgentChip agent={e.agent} waiting={e.kind === "permission" || e.kind === "question"} /> : <Box role="img" aria-label={`machine ${e.machine}`} style={{ width: 20, height: 20, borderRadius: "50%", background: "var(--driga-inset)", display: "grid", placeItems: "center", color: "var(--driga-muted)" }}><Desktop size={12} /></Box>}
      </Box>
      <Stack gap={4} style={{ flex: 1, minWidth: 0 }}>
        <Group gap={6} wrap="nowrap" align="baseline" style={{ minWidth: 0 }}>
          <Text size="md" fw={500} lh={1.2} truncate title={e.title} style={{ minWidth: 0, flex: "0 1 auto" }}>{e.title}</Text>
          <Text size="xs" c="dimmed" lh={1.2} style={{ whiteSpace: "nowrap", flex: "none" }}>{kindWord(e, now)}</Text>
        </Group>
        {e.reason && <Text size="xs" ff={e.kind === "question" ? undefined : "monospace"} className="w-inset" lh={1.4} truncate title={e.reason} style={{ display: "block", maxWidth: "100%", alignSelf: "flex-start" }}>{e.reason}</Text>}
        {where && <Text size="xs" c="dimmed" ff="monospace" lh={1.2} truncate title={where} hiddenFrom="lg">{where}</Text>}
        {e.review && <Group gap={6} wrap="wrap">
          <Text size="xs" c="dimmed" lh={1.2} style={{ whiteSpace: "nowrap" }}>{plural(e.review.files, "file")}</Text>
          {e.review.uncommitted > 0 && <Badge size="xs" variant="light" color="warning" fw={500} styles={{ root: { textTransform: "none", flex: "none" } }} leftSection={<Box aria-hidden style={{ width: 5, height: 5, borderRadius: "50%", background: "var(--mantine-color-warning-filled)" }} />}>{`${e.review.uncommitted} uncommitted`}</Badge>}
          {e.review.unpushed > 0 && <Badge size="xs" variant="light" color="gray" fw={500} styles={{ root: { textTransform: "none", flex: "none" } }}>{`${e.review.unpushed} unpushed`}</Badge>}
        </Group>}
      </Stack>
      {where && <Text size="xs" c="dimmed" ff="monospace" lh={1.2} truncate title={where} visibleFrom="lg" w={180} style={{ flex: "none", paddingTop: 2 }}>{where}</Text>}
      <Stack gap={2} align="flex-end" w={96} style={{ flex: "none" }}>
        <Text size="xs" ff="monospace" lh={1.2} title={exact(e.at)} style={{ whiteSpace: "nowrap" }}>{relative(e.at, now)}</Text>
        {waitedFor && <Group gap={4} wrap="nowrap">
          <Box aria-hidden className={e.kind === "permission" || e.kind === "question" ? "w-pulse" : undefined} style={{ width: 6, height: 6, borderRadius: "50%", background: `var(--mantine-color-${tone}-filled)`, flex: "none" }} />
          <Text size="xs" ff="monospace" lh={1.2} style={{ whiteSpace: "nowrap" }}>{e.kind === "stuck" ? "quiet" : "waiting"} {waitedFor}</Text>
        </Group>}
      </Stack>
      {(onApprove || onOpen || onDismiss) && <Group gap={4} wrap="nowrap" className="w-actions" style={{ flex: "none" }}>
        {primary && onApprove && <Tooltip label={hint(primary, "a")}><Button size="compact-xs" variant="light" onClick={() => onApprove(e)}>{primary}</Button></Tooltip>}
        {onOpen && <Tooltip label={hint("Open", "o")}><Button size="compact-xs" variant="subtle" color="gray" onClick={open}>Open</Button></Tooltip>}
        {onDismiss && <Tooltip label={hint("Dismiss", "x")}><ActionIcon size="sm" variant="subtle" color="gray" aria-label={`Dismiss ${e.title}`} onClick={() => onDismiss(e)}><X size={14} /></ActionIcon></Tooltip>}
      </Group>}
    </Group>
  </Box>;
}
