// A chat card says in a few lines what a panel tool showed or changed, and
// expands in place to the panels, stacked to fit the Chat column, or to a
// full-screen view, as Fanout's chat does. Keeping a view is something you
// ask Chat for, whose saved card then says so.
import { Badge, Button, Group, Modal, Paper, Stack, Text, UnstyledButton } from "@mantine/core";
import { ArrowsOut, CaretDown, CaretRight, ChartBar, FloppyDisk, SquaresFour } from "@phosphor-icons/react";
import { Board } from "../panels/Board";
import { useEffect, useState } from "react";
import { summary } from "../panels/charts";
import { value } from "../panels/format";
import type { Dashboard, Fragment, Panel, Result } from "../panels/types";
import { dashboards, type Operation } from "./api";

/** Card is a panel tool call: its input, and what came of it, which the
 * service reads from the tool's result: the dashboard a write saved, or why
 * the call failed. */
export type Card = {
  tool: string; input: { id?: string; dashboard?: Dashboard; vars?: Record<string, string>; operations?: Operation[]; version?: number; message?: string };
  saved?: { id: string; version: number }; error?: string;
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

/** changes lists what an edit did, in the words of its operations. */
export function changes(card: Card, before?: Dashboard): string[] {
  const title = (id: string) => before?.panels.find(p => p.id === id)?.title ?? id;
  const panels = (n: number) => `${n} ${n === 1 ? "panel" : "panels"}`;
  if (card.tool === "create_dashboard") return [`Created with ${panels(card.input.dashboard?.panels.length ?? 0)}`];
  if (card.tool === "replace_dashboard") return [`Replaced with ${panels(card.input.dashboard?.panels.length ?? 0)}`];
  if (card.tool === "restore_dashboard_version") return [`Restored version ${card.input.version ?? "?"}`];
  return (card.input.operations ?? []).map(op => {
    switch (op.op) {
      case "add": return `Added ${op.panel.title}`;
      case "update": return `Changed ${op.panel.title ?? title(op.id)}`;
      case "remove": return `Removed ${title(op.id)}`;
      case "layout": return "Moved panels";
      case "rename": return `Renamed to ${op.name}`;
    }
  });
}

export function ChatCard({ text, open: initiallyOpen, onOpen }: {
  text: string;
  /** open starts the card expanded, as the newest card does. */
  open?: boolean;
  /** onOpen opens a saved dashboard in the main area. */
  onOpen: (id: string) => void;
}) {
  const [expanded, setExpanded] = useState(!!initiallyOpen);
  const [full, setFull] = useState(false);
  const [card, setCard] = useState<Card | null>(null);
  const [fragment, setFragment] = useState<Fragment | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  useEffect(() => {
    let c: Card;
    try { c = JSON.parse(text) as Card; } catch { setProblem("This card could not be read."); return; }
    setCard(c);
    if (c.error) return;
    const spec = c.input?.dashboard;
    const id = c.saved?.id ?? c.input?.id;
    if (!spec && !id) { setProblem("This card names no dashboard."); return; }
    // A saved dashboard opens as it is now; a one-off view runs again.
    const run = id && (saves.has(c.tool) || c.tool === "open_dashboard") ? dashboards.open(id, c.input?.vars) : dashboards.run(spec!, c.input?.vars);
    run.then(setFragment, (e: Error) => setProblem(e.message));
  }, [text]);
  // A dashboard a card saved or opened may since have been deleted.
  if (problem && card && /not found/i.test(problem) && (card.saved || card.input.id)) return <Text size="sm" c="dimmed">{card.input.dashboard?.name ?? "This dashboard"} was deleted since.</Text>;
  if (problem) return <Text size="sm" c="danger">{problem}</Text>;
  // A write that failed saved nothing; say so and why, as a rejected draft.
  if (card?.error) return <Paper withBorder radius="md" p="sm" style={{ background: "var(--driga-raised)" }}>
    <Stack gap={4}>
      <Text size="sm" fw={600}>{saves.has(card.tool) ? `Draft rejected: ${card.input.dashboard?.name ?? card.input.id ?? "dashboard"}` : "These panels could not run"}</Text>
      <Text size="xs" c="dimmed" style={{ overflowWrap: "anywhere" }}>{card.error}</Text>
    </Stack>
  </Paper>;
  if (!card || !fragment) return <Text size="sm" c="dimmed">Loading panels…</Text>;
  const d = fragment.dashboard;
  const saved = saves.has(card.tool) && !!(card.saved ?? fragment.id);
  const results = new Map(fragment.results.map(r => [r.id, r]));
  const lines = d.panels;
  const done = saved ? changes(card) : [];
  return <Paper withBorder radius="md" p="sm" style={{ background: "var(--driga-raised)" }}>
    <Stack gap={6}>
      <UnstyledButton aria-expanded={expanded} onClick={() => setExpanded(e => !e)}>
        <Group gap={6} wrap="nowrap">
          {expanded ? <CaretDown size={14} aria-hidden /> : <CaretRight size={14} aria-hidden />}
          {saved ? <FloppyDisk size={16} aria-hidden /> : <ChartBar size={16} aria-hidden />}
          <Text size="sm" fw={600} truncate title={saved ? `Saved ${d.name}` : d.name}>{saved ? `Saved ${d.name}` : d.name}</Text>
          <Text size="xs" c="dimmed" style={{ whiteSpace: "nowrap" }}>{saved && (card.saved?.version ?? fragment.version) ? `version ${card.saved?.version ?? fragment.version} · ` : ""}{lines.length} {lines.length === 1 ? "panel" : "panels"}</Text>
        </Group>
      </UnstyledButton>
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
      <Group gap={4} mt={2}>
        <Button size="compact-sm" variant="subtle" color="gray" leftSection={<ArrowsOut size={14} />} onClick={() => setFull(true)}>Full screen</Button>
        {saved && <Button size="compact-sm" variant="subtle" color="gray" leftSection={<SquaresFour size={14} />} onClick={() => onOpen(fragment.id!)}>Open dashboard</Button>}
      </Group>
    </Stack>
    <Modal opened={full} onClose={() => setFull(false)} fullScreen title={saved ? `Saved ${d.name}` : d.name} radius={0}>
      <Board fragment={fragment} />
    </Modal>
  </Paper>;
}
