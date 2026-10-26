// StatusBadge is a session state in one of two forms. The default is a 6 px
// dot and the word in the text color, the only form green and yellow may
// take as text in the light scheme. The pill is for inbox kinds and
// filters: a 14% tint, a 1 px inset ring in the hue and the word. Idle's dot
// is hollow and finished is a check, so no two states differ by color alone.
import { Badge, Box, Group, Text } from "@mantine/core";
import { CheckCircle } from "@phosphor-icons/react";
import { stateTone } from "./palette";
import { stateWord, type State } from "./types";
import "./widgets.css";

export type StatusBadgeProps = {
  state: State;
  /** dot is the default; pill is the tinted form for kinds and filters. */
  variant?: "dot" | "pill";
  /** The word to show; the state's own by default. */
  label?: string;
  /** An inferred state, read from activity rather than reported by the agent. */
  inferred?: boolean;
  /** The waiting dot may breathe: 2 s, once per widget, off under reduced motion. */
  pulse?: boolean;
};

export function StatusBadge({ state, variant = "dot", label, inferred, pulse }: StatusBadgeProps) {
  const tone = stateTone[state];
  const word = label ?? stateWord[state];
  const name = inferred ? `${word}, inferred` : word;
  const color = `var(--mantine-color-${tone}-filled)`;
  const breathing = pulse && state === "waiting" ? "w-pulse" : undefined;
  const mark = state === "finished"
    ? <CheckCircle size={12} weight="regular" aria-hidden style={{ color, flex: "none" }} />
    : <Box aria-hidden className={breathing} style={{ width: 6, height: 6, borderRadius: "50%", flex: "none", background: state === "idle" ? "transparent" : color, boxShadow: state === "idle" ? `inset 0 0 0 1.5px ${color}` : undefined }} />;
  if (variant === "pill") {
    return <Badge variant="light" color={tone} size="md" radius="sm" fw={500} lts="0.04em" leftSection={mark} aria-label={name} title={name}
      styles={{ root: { textTransform: "none", boxShadow: `inset 0 0 0 1px color-mix(in srgb, ${color} 30%, transparent)`, border: inferred ? "1px dashed currentColor" : undefined, paddingInline: 6 }, label: { fontSize: "var(--mantine-font-size-xs)" } }}>
      {word}
    </Badge>;
  }
  return <Group gap={6} wrap="nowrap" aria-label={name} title={name} style={{ display: "inline-flex" }}>
    {mark}
    <Text size="xs" fw={500} lh={1.2} style={{ whiteSpace: "nowrap", borderBottom: inferred ? "1px dashed var(--driga-faint)" : undefined }}>{inferred ? `~${word}` : word}</Text>
  </Group>;
}
