// TrackerBar is a row of state cells, one per minute, poll or turn, the
// worst state in each: Zendesk's 90-day bar scaled to a machine's last 90
// minutes or a session's last 30 turns. Quiet cells stay neutral; only
// waiting and stuck take color, and a cell Driga could not see is hollow.
// The cells are DOM, which is cheaper than a canvas at this size, and one
// tooltip follows the pointer across them rather than ninety tooltips
// waiting in the tree.
import { Box, Group, Stack, Text, Tooltip } from "@mantine/core";
import { useRef, useState, type MouseEvent } from "react";
import { stateWord, type State } from "./types";
import "./widgets.css";

/** One cell: a state, offline when Driga could not reach the machine, or
 * null when nothing ran. */
export type TrackerCell = { state: State | "offline" | null; label: string };

export type TrackerBarProps = {
  cells: TrackerCell[];
  /** What the bar covers, for the accessible summary: "studio, last 90 minutes". */
  label: string;
  /** The counts said after the label; by default the cells counted by state. */
  summary?: string;
  /** The ends of the axis under the bar: "90m" and "now". */
  from?: string;
  to?: string;
  height?: number;
};

/** counted says the cells per state, so a screen reader hears the bar. */
export function counted(cells: TrackerCell[]): string {
  const counts = new Map<string, number>();
  for (const c of cells) { const k = c.state === "offline" ? "unseen" : c.state ? stateWord[c.state] : "quiet"; counts.set(k, (counts.get(k) ?? 0) + 1); }
  return [...counts.entries()].map(([k, n]) => `${n} ${k}`).join(", ");
}

export function TrackerBar({ cells, label, summary, from, to, height = 12 }: TrackerBarProps) {
  const [hover, setHover] = useState<number | null>(null);
  const ref = useRef<HTMLDivElement>(null);
  const onMove = (e: MouseEvent<HTMLDivElement>) => {
    if (!ref.current || !cells.length) return;
    const r = ref.current.getBoundingClientRect();
    setHover(Math.min(cells.length - 1, Math.max(0, Math.floor(((e.clientX - r.left) / r.width) * cells.length))));
  };
  const said = summary ?? counted(cells);
  return <Stack gap={2}>
    <Tooltip.Floating label={hover === null ? said : cells[hover].label} offset={10}>
      <Box ref={ref} role="img" aria-label={`${label}: ${said}`} onMouseMove={onMove} onMouseLeave={() => setHover(null)} style={{ display: "flex", gap: 1, height, cursor: "default" }}>
        {cells.map((c, i) => <div key={i} className="w-cell" data-state={c.state ?? undefined} style={hover === i ? { outline: "1px solid var(--driga-text)", outlineOffset: 0 } : undefined} />)}
      </Box>
    </Tooltip.Floating>
    {(from || to) && <Group justify="space-between" gap="xs" aria-hidden>
      <Text size="xs" ff="monospace" c="dimmed" lh={1}>{from}</Text>
      <Text size="xs" ff="monospace" c="dimmed" lh={1}>{to}</Text>
    </Group>}
  </Stack>;
}
