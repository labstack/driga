import { ActionIcon, Badge, Box, ColorSwatch, Group, ScrollArea, Stack, Text, Tooltip, UnstyledButton } from "@mantine/core";
import { Broom, ChartLineUp, CheckCircle, HandPalm, Hourglass, Memory, TreeStructure, X, type Icon } from "@phosphor-icons/react";
import { value } from "../panels/format";
import { Measure, Sparkline } from "../panels/PanelView";
import type { Fragment, Panel, Result } from "../panels/types";

export type Metric = { panel: Panel; result?: Result };

// The counts that mean someone must act, and the tone each takes above zero.
const alarms: Record<string, "warning" | "danger"> = { waiting: "warning", stuck: "danger" };

// Each built-in metric's icon; one Chat adds gets the chart icon.
const icons: Record<string, Icon> = { waiting: HandPalm, finished: CheckCircle, stuck: Hourglass, leftover: TreeStructure, reclaim: Broom, memory: Memory };

/** delta states a trend's change over its window in text, as Canon's stat
 * tile asks: ▲ or ▼ and the amount. */
function delta(trend: (number | null)[] | undefined, unit?: string): string | null {
  const known = (trend ?? []).filter((n): n is number => typeof n === "number");
  if (known.length < 2) return null;
  const change = known[known.length - 1] - known[0];
  if (Math.abs(change) < 1e-9) return "steady";
  return `${change > 0 ? "▲" : "▼"} ${value(Math.abs(change), unit)}`;
}

/** Metrics is the header's row of stat tiles, borderless and set apart by
 * space: an icon and label, the value with its unit, the change over the
 * last hour and a sparkline where the metric has readings over time. A
 * count that needs the person takes its tone. Clicking one asks Chat. */
export function Metrics({ fragment, editing, onAsk, onRemove }: { fragment: Fragment | null; editing: boolean; onAsk: (m: Metric) => void; onRemove: (id: string) => void }) {
  if (!fragment) return <Box style={{ flex: 1 }} />;
  return <ScrollArea type="never" style={{ flex: 1, minWidth: 0 }}>
    <Group gap={6} wrap="nowrap" component="ul" aria-label="Metrics" m={0} p={0} style={{ listStyle: "none" }}>
      {fragment.dashboard.panels.map(p => {
        const r = fragment.results.find(x => x.id === p.id);
        const n = r?.frame?.rows[0]?.[0] ?? null;
        const unit = r?.frame?.columns[0]?.unit;
        const tone = typeof n === "number" && n > 0 ? alarms[p.id] : undefined;
        const change = delta(r?.trend, unit);
        const Glyph = icons[p.id] ?? ChartLineUp;
        return <Box component="li" key={p.id} pos="relative">
          <Tooltip label={p.description ?? `Ask Chat about ${p.title.toLowerCase()}`}>
            <UnstyledButton className="metric" px={10} py={4} onClick={() => onAsk({ panel: p, result: r })}
              aria-label={`${p.title}: ${typeof n === "number" ? value(n, unit) : "no reading"}. Ask Chat about it.`}>
              <Group gap={10} wrap="nowrap" align="center">
                <Stack gap={1}>
                  <Group gap={5} wrap="nowrap" c={tone ? tone : "dimmed"}>
                    <Glyph size={13} aria-hidden />
                    <Text size="xs" c={tone ? tone : "dimmed"} lh={1.2} style={{ whiteSpace: "nowrap" }}>{p.title}</Text>
                    {tone && <ColorSwatch size={7} color={`var(--mantine-color-${tone}-filled)`} withShadow={false} aria-hidden />}
                  </Group>
                  <Group gap={6} wrap="nowrap" align="center">
                    <Box c={tone ? tone : undefined}><Measure n={n} unit={unit} size={17} /></Box>
                    {change && <Badge size="xs" color="gray" variant="light" ff="monospace">{change}</Badge>}
                  </Group>
                </Stack>
                {r?.trend && r.trend.length > 1 && <Box w={64} h={26}><Sparkline trend={r.trend} title={p.title} /></Box>}
              </Group>
            </UnstyledButton>
          </Tooltip>
          {editing && <ActionIcon size={18} radius="xl" variant="default" pos="absolute" top={-4} right={-4} aria-label={`Remove ${p.title}`} onClick={() => onRemove(p.id)}><X size={10} /></ActionIcon>}
        </Box>;
      })}
    </Group>
  </ScrollArea>;
}
