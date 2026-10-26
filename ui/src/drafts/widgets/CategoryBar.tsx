// CategoryBar is one 100% bar split into named segments, for a memory split
// such as sessions / MCP servers / agents without a session / leftovers,
// with an optional marker at a threshold. Segments sit in the categorical
// slots or a state color, with 1 px gaps so neighbours stay apart; the
// legend under the bar carries each name and value, never color alone.
import { Box, ColorSwatch, Group, Progress, Stack, Text, useComputedColorScheme } from "@mantine/core";
import { chartSeries, stateVar } from "./palette";
import type { State } from "./types";

export type Segment = {
  label: string;
  value: number;
  /** A categorical slot (0–6), the accent, or a state when the bar encodes state. */
  color?: number | "brand" | State;
};

export type CategoryBarProps = {
  segments: Segment[];
  /** The bar's whole; the segments' sum by default, so free space shows as track. */
  total?: number;
  marker?: { value: number; label: string };
  format: (n: number) => string;
  /** What the bar splits, for the legend's accessible name. */
  label: string;
  size?: number;
};

export function CategoryBar({ segments, total, marker, format, label, size = 8 }: CategoryBarProps) {
  const scheme = useComputedColorScheme("dark");
  const sum = total ?? segments.reduce((s, x) => s + x.value, 0);
  const share = (n: number) => sum > 0 ? Math.round(Math.min(100, (n / sum) * 100) * 100) / 100 : 0;
  const color = (s: Segment, i: number): string => {
    if (s.color === "brand") return "var(--mantine-color-brand-filled)";
    if (typeof s.color === "string") return stateVar(s.color);
    return chartSeries[scheme][s.color ?? i] ?? "var(--driga-faint)";
  };
  const at = marker ? share(marker.value) : 0;
  return <Stack gap={6}>
    <Box pos="relative" pt={marker ? 18 : 0}>
      <Progress.Root size={size} styles={{ root: { background: "var(--driga-border)" } }}>
        {segments.map((s, i) => <Progress.Section key={s.label} value={share(s.value)} color={color(s, i)} aria-label={`${s.label}: ${format(s.value)}`} style={{ boxShadow: "inset -1px 0 0 var(--driga-raised)" }} />)}
      </Progress.Root>
      {marker && <>
        <Box aria-hidden pos="absolute" bottom={-3} h={size + 6} w={2} left={`${at}%`} style={{ background: "var(--driga-text)", transform: "translateX(-1px)", borderRadius: 1 }} />
        <Text aria-hidden pos="absolute" top={0} size="xs" ff="monospace" c="dimmed" lh={1} style={at > 75 ? { right: 0, whiteSpace: "nowrap" } : { left: `${at}%`, transform: "translateX(-50%)", whiteSpace: "nowrap" }}>{marker.label}</Text>
      </>}
    </Box>
    <Group gap="md" role="list" aria-label={label}>
      {segments.map((s, i) => <Group key={s.label} role="listitem" gap={6} wrap="nowrap">
        <ColorSwatch size={8} color={color(s, i)} withShadow={false} aria-hidden />
        <Text size="xs" lh={1.2}>{s.label}</Text>
        <Text size="xs" ff="monospace" c="dimmed" lh={1.2}>{format(s.value)}</Text>
      </Group>)}
      {marker && <Text role="listitem" size="xs" c="dimmed" lh={1.2}>{marker.label} at {format(marker.value)}</Text>}
    </Group>
  </Stack>;
}
