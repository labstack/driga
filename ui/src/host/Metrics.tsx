import { ActionIcon, Box, Group, Paper, ScrollArea, Text, Tooltip, UnstyledButton } from "@mantine/core";
import { X } from "@phosphor-icons/react";
import { value } from "../panels/format";
import { Measure, Sparkline } from "../panels/PanelView";
import type { Fragment, Panel, Result } from "../panels/types";

export type Metric = { panel: Panel; result?: Result };

// The counts that mean someone must act, and the tone each takes above zero.
const alarms: Record<string, "warning" | "danger"> = { waiting: "warning", stuck: "danger" };

/** delta states a trend's change over its window in text, as Canon's stat
 * tile asks: ▲ or ▼ and the amount. */
function delta(trend: (number | null)[] | undefined, unit?: string): string | null {
  const known = (trend ?? []).filter((n): n is number => typeof n === "number");
  if (known.length < 2) return null;
  const change = known[known.length - 1] - known[0];
  if (Math.abs(change) < 1e-9) return "no change in 1h";
  return `${change > 0 ? "▲" : "▼"} ${value(Math.abs(change), unit)} in 1h`;
}

/** Metrics is the header's row of stat tiles: label, value with its unit,
 * the change over the last hour and a sparkline where the metric has
 * readings over time. Clicking one asks Chat about it. */
export function Metrics({ fragment, editing, onAsk, onRemove }: { fragment: Fragment | null; editing: boolean; onAsk: (m: Metric) => void; onRemove: (id: string) => void }) {
  if (!fragment) return <Box style={{ flex: 1 }} />;
  return <ScrollArea type="never" style={{ flex: 1, minWidth: 0 }}>
    <Group gap={8} wrap="nowrap" component="ul" aria-label="Metrics" m={0} p={0} style={{ listStyle: "none" }}>
      {fragment.dashboard.panels.map(p => {
        const r = fragment.results.find(x => x.id === p.id);
        const n = r?.frame?.rows[0]?.[0] ?? null;
        const unit = r?.frame?.columns[0]?.unit;
        const tone = typeof n === "number" && n > 0 ? alarms[p.id] : undefined;
        const change = delta(r?.trend, unit);
        return <Box component="li" key={p.id} pos="relative">
          <Tooltip label={p.description ?? `Ask Chat about ${p.title.toLowerCase()}`}>
            <UnstyledButton onClick={() => onAsk({ panel: p, result: r })} aria-label={`${p.title}: ${typeof n === "number" ? value(n, unit) : "no reading"}. Ask Chat about it.`}>
              <Paper withBorder px={10} py={5} radius="sm" className="metric"
                style={{ borderColor: tone ? `var(--mantine-color-${tone}-filled)` : undefined, background: tone ? `var(--mantine-color-${tone}-light)` : undefined }}>
                <Group gap={10} wrap="nowrap">
                  <Box>
                    <Text size="xs" c="dimmed" lh={1.2} truncate>{p.title}</Text>
                    <Group gap={6} wrap="nowrap" align="baseline">
                      <Measure n={n} unit={unit} size={16} />
                      {change && <Text size="xs" c="dimmed" ff="monospace" style={{ whiteSpace: "nowrap" }}>{change}</Text>}
                    </Group>
                  </Box>
                  {r?.trend && r.trend.length > 1 && <Box w={72} h={28}><Sparkline trend={r.trend} title={p.title} /></Box>}
                </Group>
              </Paper>
            </UnstyledButton>
          </Tooltip>
          {editing && <ActionIcon size={18} radius="xl" variant="default" pos="absolute" top={-6} right={-6} aria-label={`Remove ${p.title}`} onClick={() => onRemove(p.id)}><X size={10} /></ActionIcon>}
        </Box>;
      })}
    </Group>
  </ScrollArea>;
}
