import { ActionIcon, Badge, Box, ColorSwatch, Group, Menu, ScrollArea, Stack, Text, UnstyledButton } from "@mantine/core";
import { Broom, ChartLineUp, ChatCircle, CheckCircle, HandPalm, Hourglass, Memory, SquaresFour, TreeStructure, X, type Icon } from "@phosphor-icons/react";
import { value } from "../panels/format";
import { Measure, Sparkline } from "../panels/PanelView";
import type { Fragment, Panel, Result } from "../panels/types";

export type Metric = { panel: Panel; result?: Result };

// The counts that mean someone must act, and the tone each takes above zero.
const alarms: Record<string, "warning" | "danger"> = { waiting: "warning", stuck: "danger" };

// The Home panel each built-in metric is about, which its tile shows.
export const shows: Record<string, string> = { waiting: "inbox", finished: "inbox", stuck: "stuck", leftover: "orphans", memory: "machine_memory" };

// Each built-in metric's icon; one Chat adds gets the chart icon.
const icons: Record<string, Icon> = { waiting: HandPalm, finished: CheckCircle, stuck: Hourglass, leftover: TreeStructure, reclaim: Broom, memory: Memory };

/** delta states a trend's change over its window in text, as Canon's stat
 * tile asks: ▲ or ▼ and the amount. */
function delta(trend: (number | null)[] | undefined, unit?: string): string | null {
  const known = (trend ?? []).filter((n): n is number => typeof n === "number");
  if (known.length < 2) return null;
  const change = known[known.length - 1] - known[0];
  if (Math.abs(change) < 1e-9) return "steady 1h";
  return `${change > 0 ? "▲" : "▼"} ${value(Math.abs(change), unit)} 1h`;
}

/** Metrics is the header's row of stat tiles, borderless and set apart by
 * space: an icon and label, the value with its unit, the change over the
 * last hour and a sparkline where the metric has readings over time. A
 * count that needs the person takes its tone. Clicking one asks Chat. */
export function Metrics({ fragment, editing, extra, onAsk, onShow, onRemove }: { fragment: Fragment | null; editing: boolean; extra?: Record<string, number>; onAsk: (m: Metric) => void; onShow?: (panel: string) => void; onRemove: (id: string) => void }) {
  if (!fragment) return <Box style={{ flex: 1 }} />;
  return <ScrollArea type="never" style={{ flex: 1, minWidth: 0 }}>
    <Group gap={6} wrap="nowrap" component="ul" aria-label="Metrics" m={0} p={0} style={{ listStyle: "none" }}>
      {fragment.dashboard.panels.map(p => {
        const r = fragment.results.find(x => x.id === p.id);
        // extra adds what the panel's source can't see, such as Chat's
        // permission prompts to the waits that need the person.
        const read = r?.frame?.rows[0]?.[0] ?? null;
        const n = typeof read === "number" && extra?.[p.id] ? read + extra[p.id] : read;
        const unit = r?.frame?.columns[0]?.unit;
        const tone = typeof n === "number" && n > 0 ? alarms[p.id] : undefined;
        const change = delta(r?.trend, unit);
        const Glyph = icons[p.id] ?? ChartLineUp;
        return <Box component="li" key={p.id} pos="relative">
          <Menu position="bottom-start" withinPortal shadow="md" width={200}>
            <Menu.Target>
            <UnstyledButton className="metric" px={10} py={4}
              aria-label={`${p.title}: ${typeof n === "number" ? value(n, unit) : "no reading"}`}>
              <Group gap={10} wrap="nowrap" align="center">
                <Stack gap={1}>
                  <Group gap={5} wrap="nowrap" c={tone ? tone : "dimmed"}>
                    <Glyph size={13} aria-hidden />
                    <Text size="xs" c={tone ? tone : "dimmed"} lh={1.2} style={{ whiteSpace: "nowrap" }}>{p.title}</Text>
                    {tone && <ColorSwatch size={7} color={`var(--mantine-color-${tone}-filled)`} withShadow={false} aria-hidden />}
                  </Group>
                  <Group gap={6} wrap="nowrap" align="center">
                    <Box c={tone ? tone : undefined}><Measure n={n} unit={unit} size={17} total={r?.total} /></Box>
                    {change && <Badge size="xs" color="gray" variant="light" ff="monospace">{change}</Badge>}
                  </Group>
                </Stack>
                {r?.trend && r.trend.length > 1 && <Box w={64} h={26}><Sparkline trend={r.trend} title={p.title} /></Box>}
              </Group>
            </UnstyledButton>
            </Menu.Target>
            <Menu.Dropdown>
              {p.description && <Menu.Label style={{ textTransform: "none", whiteSpace: "normal" }}>{p.description}</Menu.Label>}
              {onShow && shows[p.id] && <Menu.Item leftSection={<SquaresFour />} onClick={() => onShow(shows[p.id])}>Show on Home</Menu.Item>}
              <Menu.Item leftSection={<ChatCircle />} onClick={() => onAsk({ panel: p, result: r })}>Ask Chat about it</Menu.Item>
            </Menu.Dropdown>
          </Menu>
          {editing && <ActionIcon size={18} radius="xl" variant="default" pos="absolute" top={-4} right={-4} aria-label={`Remove ${p.title}`} onClick={() => onRemove(p.id)}><X size={10} /></ActionIcon>}
        </Box>;
      })}
    </Group>
  </ScrollArea>;
}
