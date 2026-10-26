import { Text, VisuallyHidden } from "@mantine/core";
import type { Polarity } from "./Header";

const tints: Record<Polarity, string> = {
  good: "color-mix(in srgb, var(--mantine-color-success-filled) 14%, transparent)",
  bad: "color-mix(in srgb, var(--mantine-color-danger-filled) 14%, transparent)",
  neutral: "color-mix(in srgb, var(--driga-faint) 14%, transparent)",
};

/** DeltaChip states a change over a window in text, as Canon's stat tile
 * asks: ▲ or ▼ and the amount, in a tint that says whether it is good news.
 * All of its text keeps the text color, so it reads on the tint in both
 * schemes. */
export function DeltaChip({ delta, amount, polarity, window }: { delta: number; amount: string; polarity: Polarity; window?: string }) {
  const arrow = delta > 0 ? "▲" : delta < 0 ? "▼" : null;
  return <Text component="span" fz={12} ff="monospace" lh={1.4} px={5} style={{ display: "inline-flex", alignItems: "center", gap: 4, borderRadius: "var(--mantine-radius-sm)", background: tints[polarity], whiteSpace: "nowrap", fontVariantNumeric: "tabular-nums" }}>
    {arrow && <><span aria-hidden="true">{arrow}</span><VisuallyHidden>{delta > 0 ? " up " : " down "}</VisuallyHidden></>}
    {delta === 0 ? "steady" : amount}{window && ` · ${window}`}
  </Text>;
}
