import { Text, VisuallyHidden } from "@mantine/core";
import "./widgets.css";

/** Whether a change is good news: it decides nothing visible, since green
 * and red mean working and stuck, but the chip says it in words. */
export type Polarity = "good" | "bad" | "neutral";

/** change says a delta in words: "1.8 GB less than 1h ago, good". */
export function change(delta: number, amount: string, polarity: Polarity, window = "1h"): string {
  if (delta === 0) return `No change over the last ${window}`;
  return `${amount} ${delta > 0 ? "more" : "less"} than ${window} ago${polarity === "good" ? ", good" : polarity === "bad" ? ", worse" : ""}`;
}

/** DeltaChip states a change over a window in text, as Canon's stat tile
 * asks: ▲ or ▼ and the amount, in the neutral tint. The arrow carries the
 * direction; whether it is good news is in the words a screen reader and
 * the tooltip get, never in a color. */
export function DeltaChip({ delta, amount, polarity, window = "1h" }: { delta: number; amount: string; polarity: Polarity; window?: string }) {
  const arrow = delta > 0 ? "▲" : delta < 0 ? "▼" : null;
  const said = change(delta, amount, polarity, window);
  return <Text component="span" fz={12} ff="monospace" lh={1.4} px={5} title={said} style={{ display: "inline-flex", alignItems: "center", gap: 4, borderRadius: "var(--mantine-radius-sm)", background: "var(--driga-tint-neutral)", whiteSpace: "nowrap", fontVariantNumeric: "tabular-nums" }}>
    <span aria-hidden="true">{arrow && `${arrow} `}{delta === 0 ? "steady" : amount}{window && ` · ${window}`}</span>
    <VisuallyHidden>{said}</VisuallyHidden>
  </Text>;
}
