// A chat card says in a few lines what a panel tool showed or changed, and
// expands in place to the panels, stacked to fit the Chat column, or to a
// full-screen view, as Fanout's chat does. Keeping a view is something you
// ask Chat for, whose saved card then says so.
import { ActionIcon, Badge, Group, Modal, Paper, Stack, Text, Tooltip, UnstyledButton } from "@mantine/core";
import { ArrowsOut, CaretDown, CaretRight, ChartBar, FloppyDisk, SquaresFour } from "@phosphor-icons/react";
import { Board } from "../panels/Board";
import { useEffect, useRef, useState } from "react";
import { summary } from "../panels/charts";
import { value } from "../panels/format";
import type { Dashboard, Fragment, Panel, Result } from "../panels/types";
import { APIError, dashboards, type Operation } from "./api";

/** Card is a panel tool call: its input, and what came of it, which the
 * service reads from the tool's result: the dashboard a write saved, or why
 * the call failed. */
export type Card = {
  tool: string; input: { id?: string; dashboard?: Dashboard; vars?: Record<string, string>; operations?: Operation[]; version?: number; message?: string };
  saved?: { id: string; version: number }; error?: string; status?: string;
};

// Tools whose card is a saved dashboard rather than a one-off view.
const saves = new Set(["create_dashboard", "edit_dashboard", "replace_dashboard", "restore_dashboard_version"]);

/** reading is one panel's result in a few words: a stat's value, a chart's
 * summary, how many rows a list holds, or why it failed. */
export function reading(panel: Panel, result?: Result): string {
  // A text panel is written, not run: say what it holds.
  if (panel.viz === "text") {
    const rows = (panel.content ?? "").split("\n").filter(l => /^\s*\|/.test(l) && !/^\s*\|[\s:|-]+\|\s*$/.test(l)).length;
    return rows > 1 ? `table, ${rows - 1} rows` : "note";
  }
  if (!result) return "loading";
  if (result.status === "error") return result.message ?? "failed";
  if (result.status === "empty" || !result.frame?.rows.length) return result.message ?? "nothing right now";
  const frame = result.frame;
  if (panel.viz === "stat" || panel.viz === "gauge") {
    const n = frame.rows[0]?.[0];
    const unit = frame.columns[0]?.unit;
    if (typeof n !== "number") return "no reading";
    return typeof result.total === "number" ? `${value(n, unit)} of ${value(result.total, unit)}` : value(n, unit);
  }
  if (panel.viz === "bar" || panel.viz === "timeseries") return summary(frame, panel) || `${frame.rows.length} points`;
  return `${frame.rows.length} ${frame.rows.length === 1 ? "row" : "rows"}`;
}

/** problems says how many of a card's panels failed, or nothing: what the
 * card holds its panels say themselves. */
export function problems(results: Result[]): string {
  const failed = results.filter(r => r.status === "error").length;
  return failed ? `${failed} failed` : "";
}

/** changes lists what an edit did, in the words of its operations. */
export function changes(card: Card, before?: Dashboard): string[] {
  const title = (id: string) => before?.panels.find(p => p.id === id)?.title ?? id;
  if (card.tool === "create_dashboard") return ["Created"];
  if (card.tool === "replace_dashboard") return ["Replaced"];
  if (card.tool === "restore_dashboard_version") return [`Restored version ${card.input.version ?? "?"}`];
  return (card.input.operations ?? []).map(op => {
    switch (op.op) {
      case "add": return `Added ${op.panel.title}`;
      case "update": return `Changed ${op.panel?.title ?? title(op.id)}`;
      case "remove": return `Removed ${title(op.id)}`;
      case "layout": return "Moved panels";
      case "rename": return `Renamed to ${op.name}`;
    }
  });
}

export function ChatCard({ text, open: initiallyOpen, onOpen }: {
  text: string;
  /** open follows the newest card until the user chooses whether to expand it. */
  open?: boolean;
  /** onOpen opens a saved dashboard in the main area. */
  onOpen: (id: string) => void;
}) {
  const [expanded, setExpanded] = useState(!!initiallyOpen);
  const toggled = useRef(false);
  useEffect(() => { if (!toggled.current) setExpanded(!!initiallyOpen); }, [initiallyOpen]);
  const [deleted, setDeleted] = useState(false);
  const [full, setFull] = useState(false);
  const [card, setCard] = useState<Card | null>(null);
  const [fragment, setFragment] = useState<Fragment | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  useEffect(() => {
    const control = new AbortController();
    setProblem(null); setFragment(null); setDeleted(false);
    let c: Card;
    try { c = JSON.parse(text) as Card; } catch { setProblem("This card could not be read."); return; }
    setCard(c);
    if (c.error) return;
    const spec = c.input?.dashboard;
    const id = c.saved?.id ?? c.input?.id;
    if (!spec && !id) { setProblem("This card names no dashboard."); return; }
    // A saved dashboard opens as it is now; a one-off view runs again.
    const run = id && (saves.has(c.tool) || c.tool === "open_dashboard") ? dashboards.open(id, c.input?.vars, control.signal) : dashboards.run(spec!, c.input?.vars, control.signal);
    run.then(f => { if (!control.signal.aborted) setFragment(f); }, (e: Error) => { if (!control.signal.aborted) { setDeleted(e instanceof APIError && e.status === 404); setProblem(e.message); } });
    return () => control.abort();
  }, [text]);
  // A dashboard a card saved or opened may since have been deleted.
  if (problem && card && deleted && (card.saved || card.input.id)) return <Text size="sm" c="dimmed">{card.input.dashboard?.name ?? "This dashboard"} was deleted since.</Text>;
  if (problem) return <Text size="sm" c="danger">{problem}</Text>;
  // A write that failed saved nothing; say so and why, as a rejected draft.
  if (card?.error) return <Paper withBorder radius="md" p="sm" style={{ background: "var(--driga-raised)" }}>
    <Stack gap={4}>
      <Text size="sm" fw={600}>{card.status === "unknown" ? `Outcome unknown: ${card.input.dashboard?.name ?? card.input.id ?? "dashboard"}` : saves.has(card.tool) ? `Draft rejected: ${card.input.dashboard?.name ?? card.input.id ?? "dashboard"}` : "These panels could not run"}</Text>
      <Text size="xs" c="dimmed" style={{ overflowWrap: "anywhere" }}>{card.error}</Text>
    </Stack>
  </Paper>;
  if (!card || !fragment) return <Text size="sm" c="dimmed">Loading panels…</Text>;
  const d = fragment.dashboard;
  const saved = saves.has(card.tool) && !!(card.saved ?? fragment.id);
  const results = new Map(fragment.results.map(r => [r.id, r]));
  const lines = d.panels;
  const done = saved ? changes(card) : [];
  const version = saved ? card.saved?.version ?? fragment.version : undefined;
  const failed = problems(fragment.results);
  const name = saved ? `Saved ${d.name}` : d.name;
  return <Paper withBorder radius="md" p="sm" style={{ background: "var(--driga-raised)" }}>
    <Stack gap={6}>
      <Group gap={4} wrap="nowrap">
        <UnstyledButton aria-expanded={expanded} onClick={() => { toggled.current = true; setExpanded(e => !e); }} style={{ flex: 1, minWidth: 0 }}>
          <Group gap={6} wrap="nowrap">
            {expanded ? <CaretDown size={14} aria-hidden /> : <CaretRight size={14} aria-hidden />}
            {saved ? <FloppyDisk size={16} aria-hidden /> : <ChartBar size={16} aria-hidden />}
            <Text size="sm" fw={600} truncate title={name}>{name}</Text>
            {version ? <Text size="xs" c="dimmed" style={{ whiteSpace: "nowrap" }}>version {version}</Text> : null}
            {failed && <Text size="xs" c="danger" style={{ whiteSpace: "nowrap" }}>{failed}</Text>}
          </Group>
        </UnstyledButton>
        {saved && <Tooltip label="Open dashboard"><ActionIcon size="sm" aria-label="Open dashboard" onClick={() => onOpen(fragment.id!)}><SquaresFour size={16} /></ActionIcon></Tooltip>}
        <Tooltip label="Full screen"><ActionIcon size="sm" aria-label="Full screen" onClick={() => setFull(true)}><ArrowsOut size={16} /></ActionIcon></Tooltip>
      </Group>
      {done.length > 0 && <Group gap={4}>{done.map(c => <Badge key={c} size="sm" variant="light" color="gray" tt="none">{c}</Badge>)}</Group>}
      {expanded ? <Board fragment={fragment} compact stacked bare /> : <Stack gap={2} component="ul" m={0} p={0} style={{ listStyle: "none" }}>
        {lines.slice(0, 6).map(p => {
          const r = results.get(p.id);
          return <Group component="li" key={p.id} gap={8} wrap="nowrap" justify="space-between">
            <Text size="xs" truncate title={r?.title || p.title}>{r?.title || p.title}</Text>
            <Text size="xs" c={r?.status === "error" ? "danger" : "dimmed"} ff="monospace" truncate ta="right" style={{ maxWidth: "55%" }} title={reading(p, r)}>{reading(p, r)}</Text>
          </Group>;
        })}
        {lines.length > 6 && <Text component="li" size="xs" c="dimmed">and {lines.length - 6} more</Text>}
      </Stack>}
    </Stack>
    <Modal opened={full} onClose={() => setFull(false)} fullScreen title={name} radius={0}>
      <Board fragment={fragment} />
    </Modal>
  </Paper>;
}
