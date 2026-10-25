// A chat card says in a few lines what a panel tool showed or changed. The
// panels themselves need the room of the main area, so Open shows them
// there; the narrow Chat column never draws a table or a chart.
import { Badge, Button, Group, Paper, Stack, Text } from "@mantine/core";
import { ArrowSquareOut, ChartBar, FloppyDisk, PushPin, SquaresFour } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { summary } from "../panels/charts";
import { value } from "../panels/format";
import type { Dashboard, Fragment, Panel, Result } from "../panels/types";
import { dashboards, type Operation } from "./api";

export type Card = { tool: string; input: { id?: string; dashboard?: Dashboard; vars?: Record<string, string>; operations?: Operation[]; version?: number; message?: string } };

// Tools whose card is a saved dashboard rather than a one-off view.
const saves = new Set(["create_dashboard", "edit_dashboard", "replace_dashboard", "restore_dashboard_version"]);

/** reading is one panel's result in a few words: a stat's value, a chart's
 * summary, how many rows a list holds, or why it failed. */
export function reading(panel: Panel, result?: Result): string {
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
  if (panel.viz === "text") return "";
  return `${frame.rows.length} ${frame.rows.length === 1 ? "row" : "rows"}`;
}

/** changes lists what an edit did, in the words of its operations. */
export function changes(card: Card, before?: Dashboard): string[] {
  const title = (id: string) => before?.panels.find(p => p.id === id)?.title ?? id;
  if (card.tool === "create_dashboard") return [`Created with ${card.input.dashboard?.panels.length ?? 0} panels`];
  if (card.tool === "replace_dashboard") return [`Replaced with ${card.input.dashboard?.panels.length ?? 0} panels`];
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

export function ChatCard({ text, pinLabel, onShow, onOpen, onPin, onSave }: {
  text: string; pinLabel: string;
  /** onShow puts a one-off view in the main area; onOpen opens a saved dashboard there. */
  onShow: (f: Fragment) => void; onOpen: (id: string) => void;
  onPin: (panels: Panel[]) => void; onSave: (panels: Panel[], name: string) => void;
}) {
  const [card, setCard] = useState<Card | null>(null);
  const [fragment, setFragment] = useState<Fragment | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  useEffect(() => {
    let c: Card;
    try { c = JSON.parse(text) as Card; } catch { setProblem("This card could not be read."); return; }
    setCard(c);
    const spec = c.input?.dashboard;
    const id = c.input?.id;
    if (!spec && !id) { setProblem("This card names no dashboard."); return; }
    // A saved dashboard opens as it is now; a one-off view runs again.
    const run = id && (saves.has(c.tool) || c.tool === "open_dashboard") ? dashboards.open(id, c.input?.vars) : dashboards.run(spec!, c.input?.vars);
    run.then(setFragment, (e: Error) => setProblem(e.message));
  }, [text]);
  if (problem) return <Text size="sm" c="danger">{problem}</Text>;
  if (!card || !fragment) return <Text size="sm" c="dimmed">Loading panels…</Text>;
  const d = fragment.dashboard;
  const saved = saves.has(card.tool) && !!fragment.id;
  const results = new Map(fragment.results.map(r => [r.id, r]));
  const lines = d.panels.filter(p => p.viz !== "text");
  const done = saved ? changes(card) : [];
  return <Paper withBorder radius="md" p="sm" style={{ background: "var(--driga-raised)" }}>
    <Stack gap={6}>
      <Group gap={6} wrap="nowrap">
        {saved ? <FloppyDisk size={16} aria-hidden /> : <ChartBar size={16} aria-hidden />}
        <Text size="sm" fw={600} truncate>{saved ? `Saved ${d.name}` : d.name}</Text>
        <Text size="xs" c="dimmed" style={{ whiteSpace: "nowrap" }}>{saved && fragment.version ? `version ${fragment.version} · ` : ""}{lines.length} {lines.length === 1 ? "panel" : "panels"}</Text>
      </Group>
      {done.length > 0 && <Group gap={4}>{done.map(c => <Badge key={c} size="sm" variant="light" color="gray" tt="none">{c}</Badge>)}</Group>}
      <Stack gap={2} component="ul" m={0} p={0} style={{ listStyle: "none" }}>
        {lines.slice(0, 6).map(p => {
          const r = results.get(p.id);
          return <Group component="li" key={p.id} gap={8} wrap="nowrap" justify="space-between">
            <Text size="xs" truncate>{r?.title || p.title}</Text>
            <Text size="xs" c={r?.status === "error" ? "danger" : "dimmed"} ff="monospace" truncate ta="right" style={{ maxWidth: "55%" }}>{reading(p, r)}</Text>
          </Group>;
        })}
        {lines.length > 6 && <Text component="li" size="xs" c="dimmed">and {lines.length - 6} more</Text>}
      </Stack>
      <Group gap={4} mt={2}>
        <Button size="compact-sm" variant="light" leftSection={saved ? <SquaresFour size={14} /> : <ArrowSquareOut size={14} />} onClick={() => saved ? onOpen(fragment.id!) : onShow(fragment)}>{saved ? "Open dashboard" : "Open"}</Button>
        {!saved && <Button size="compact-sm" variant="subtle" color="gray" leftSection={<PushPin size={14} />} onClick={() => onPin(d.panels)}>{pinLabel}</Button>}
        {!saved && <Button size="compact-sm" variant="subtle" color="gray" leftSection={<FloppyDisk size={14} />} onClick={() => onSave(d.panels, d.name)}>Save</Button>}
      </Group>
    </Stack>
  </Paper>;
}
