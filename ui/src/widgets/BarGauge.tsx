// BarGauge is a thin pressure bar for memory, swap or disk: the used value
// over the total in mono on the left, the share on the right, and a track
// whose fill turns from the accent to warning at 70% and danger at 90%. Two
// ticks on the track mark the thresholds, so the thresholds are learned by
// looking. It fits a header cluster, a table cell or a machine card.
import { Box, Group, Progress, Stack, Text } from "@mantine/core";

export type Pressure = "brand" | "warning" | "danger";

export type BarGaugeProps = {
  value: number;
  max: number;
  /** How a value reads: bytes, a percent, a count. */
  format: (n: number) => string;
  /** What is measured, for the accessible name: "Memory on studio". */
  label: string;
  /** Shares at which the fill turns warning, then danger. */
  thresholds?: [number, number];
  /** Track height in pixels. */
  size?: number;
  /** Show value/total and share, only the percent, or no caption. */
  caption?: boolean | "percent";
};

/** pressureTone is the color a memory bar takes: the accent, then warning
 * from the first threshold and danger from the second. The header passes
 * one threshold twice, so its bar is brand until danger. */
export function pressureTone(share: number, [warn, danger]: [number, number] = [0.7, 0.9]): Pressure {
  return share >= danger ? "danger" : share >= warn ? "warning" : "brand";
}

export function BarGauge({ value, max, format, label, thresholds = [0.7, 0.9], size = 4, caption = true }: BarGaugeProps) {
  const share = max > 0 ? Math.min(1, Math.max(0, value / max)) : 0;
  const pct = Math.round(share * 100);
  const tone = pressureTone(share, thresholds);
  const name = `${label}: ${format(value)} of ${format(max)}, ${pct}%`;
  return <Stack gap={4}>
    {caption && <Group justify={caption === "percent" ? "flex-end" : "space-between"} gap="xs" wrap="nowrap" aria-hidden>
      {caption !== "percent" && <Text size="xs" ff="monospace" lh={1.2} style={{ whiteSpace: "nowrap" }}>{format(value)} <Text span c="dimmed" ff="monospace" fz="inherit">/ {format(max)}</Text></Text>}
      <Text size="xs" ff="monospace" c={tone === "brand" ? "dimmed" : tone} lh={1.2}>{pct}%</Text>
    </Group>}
    <Box pos="relative" py={2}>
      <Progress.Root size={size} styles={{ root: { background: "var(--driga-border)" } }}>
        <Progress.Section value={share * 100} color={tone} aria-label={name} />
      </Progress.Root>
      {thresholds.map(t => <Box key={t} aria-hidden pos="absolute" top={0} h={size + 4} w={1} left={`${t * 100}%`} style={{ background: "var(--driga-control-border)" }} />)}
    </Box>
  </Stack>;
}
