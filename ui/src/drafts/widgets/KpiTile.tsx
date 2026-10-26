// KpiTile is a stat with context: a tracked label, a large mono value with
// its unit, a delta chip whose color reads the metric's own polarity (down
// is good for memory, bad for sessions), and a sparkline. A zero is quiet:
// grey value, no chip. A count that needs the person tints the tile in its
// state color with a 2 px left rule and a dot; the waiting dot may breathe.
// Clickable tiles are buttons and show their selection as an inset ring.
import { Box, ColorSwatch, Group, Stack, Text, UnstyledButton, useComputedColorScheme } from "@mantine/core";
import { CaretDown, CaretUp, Minus, type Icon } from "@phosphor-icons/react";
import { useCallback } from "react";
import { Chart } from "../../panels/Chart";
import { tones, type Tone } from "../../theme/tokens";
import { sparkline } from "./charts";
import "./widgets.css";

export type Attention = "waiting" | "review" | "stuck";

/** Which direction is good for this metric: up for working sessions, down for memory, neither for a count of turns. */
export type Polarity = "up-good" | "down-good" | "neutral";

export type Delta = { change: number; window: string };

export type KpiTileProps = {
  label: string;
  icon?: Icon;
  value: number | null;
  unit?: string;
  format?: (n: number) => string;
  delta?: Delta;
  polarity?: Polarity;
  /** Readings over the window, newest last, for the sparkline. */
  trend?: (number | null)[];
  /** The kind of attention a count above zero means; the tile tints in its color. */
  attention?: Attention;
  /** What the tile means, on hover. */
  description?: string;
  selected?: boolean;
  onClick?: () => void;
};

const attentionTone: Record<Attention, Tone> = { waiting: "warning", review: "info", stuck: "danger" };

export function KpiTile({ label, icon: Glyph, value, unit, format = String, delta, polarity = "neutral", trend, attention, description, selected, onClick }: KpiTileProps) {
  const scheme = useComputedColorScheme("dark");
  const zero = value === null || value === 0;
  const tone = attention && typeof value === "number" && value > 0 ? attentionTone[attention] : undefined;
  const line = tone ? tones[scheme][tone] : tones[scheme].brand;
  const option = useCallback(() => sparkline(trend ?? [], line, `${label} over the window`), [trend, line, label]);
  const change = delta?.change ?? 0;
  const flat = Math.abs(change) < 1e-9;
  const good = polarity === "neutral" || flat ? null : (change > 0) === (polarity === "up-good");
  const chipTone = good === null ? "gray" : good ? "success" : "danger";
  const Caret = flat ? Minus : change > 0 ? CaretUp : CaretDown;
  const chipText = flat ? `steady · ${delta?.window}` : `${format(Math.abs(change))} · ${delta?.window}`;
  const chipTitle = flat ? `No change over the last ${delta?.window}` : `${format(Math.abs(change))} ${change > 0 ? "more" : "less"} than ${delta?.window} ago${good === null ? "" : good ? ", good" : ", worse"}`;
  const Root = onClick ? UnstyledButton : Box;
  return <Root className="w-tile" data-clickable={onClick ? "" : undefined} data-selected={selected ? "true" : undefined} data-attention={tone} aria-pressed={onClick ? !!selected : undefined} onClick={onClick} title={description} p="sm">
    <Stack gap={4}>
      <Group gap={6} wrap="nowrap">
        {Glyph && <Glyph size={14} aria-hidden style={{ color: tone ? `var(--mantine-color-${tone}-filled)` : "var(--driga-muted)", flex: "none" }} />}
        <Text size="xs" fw={500} tt="uppercase" lts="0.04em" c="dimmed" lh={1.2} truncate title={label}>{label}</Text>
      </Group>
      <Group gap={6} wrap="nowrap" align="center">
        {tone && <ColorSwatch size={6} color={`var(--mantine-color-${tone}-filled)`} withShadow={false} aria-hidden className={attention === "waiting" ? "w-pulse" : undefined} style={{ flex: "none" }} />}
        <Text ff="monospace" fz={22} fw={500} lh={1.1} c={zero ? "dimmed" : undefined} style={{ fontVariantNumeric: "tabular-nums", whiteSpace: "nowrap" }}>{value === null ? "–" : format(value)}</Text>
        {unit && <Text ff="monospace" size="xs" c="dimmed" lh={1} style={{ alignSelf: "flex-end", paddingBottom: 3 }}>{unit}</Text>}
      </Group>
      <Group justify="space-between" wrap="nowrap" gap={6} h={24}>
        {delta && !zero
          ? <span className="w-chip" data-tone={chipTone} title={chipTitle} aria-label={chipTitle} style={{ flex: "none" }}><Caret size={10} aria-hidden />{chipText}</span>
          : <span />}
        {trend && trend.length > 1 && <Box h={24} style={{ flex: "1 1 48px", minWidth: 40, maxWidth: 96 }}><Chart option={option} label={`${label} over the window`} small /></Box>}
      </Group>
    </Stack>
  </Root>;
}
