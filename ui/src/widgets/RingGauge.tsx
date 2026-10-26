// RingGauge is the bar gauge's round form for a machine card: a 44 px ring,
// 4 px thick, the share in its centre in mono and the value over the total
// beside it. Same thresholds as the bar: accent, warning at 70%, danger at
// 90%. Mantine draws it in the DOM, so it costs no canvas.
import { Group, RingProgress, Stack, Text } from "@mantine/core";
import { pressureTone } from "./BarGauge";

export type RingGaugeProps = {
  value: number;
  max: number;
  format: (n: number) => string;
  /** What is measured: "Memory", "Disk". Shown beside the ring. */
  label: string;
  /** A second line under the label; the value over the total by default. */
  caption?: string;
  thresholds?: [number, number];
  size?: number;
  thickness?: number;
};

export function RingGauge({ value, max, format, label, caption, thresholds, size = 44, thickness = 4 }: RingGaugeProps) {
  const share = max > 0 ? Math.min(1, Math.max(0, value / max)) : 0;
  const pct = Math.round(share * 100);
  const tone = pressureTone(share, thresholds);
  const detail = caption ?? `${format(value)} / ${format(max)}`;
  return <Group gap="sm" wrap="nowrap">
    <RingProgress size={size} thickness={thickness} rootColor="var(--driga-border)" sections={[{ value: pct, color: tone }]} role="img" aria-label={`${label}: ${detail}, ${pct}%`} style={{ flex: "none" }}
      label={<Text ta="center" size="xs" ff="monospace" fw={500} lh={1} aria-hidden>{pct}%</Text>} />
    <Stack gap={2} style={{ minWidth: 0 }} aria-hidden>
      <Text size="sm" fw={500} lh={1.2} truncate title={label}>{label}</Text>
      <Text size="xs" c="dimmed" ff="monospace" lh={1.2} style={{ whiteSpace: "nowrap" }}>{detail}</Text>
    </Stack>
  </Group>;
}
