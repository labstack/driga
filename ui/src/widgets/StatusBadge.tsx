// StatusBadge is a session state in one of two forms. The default is a 6 px
// dot and the word in the text color, the form tables, cards, legends and
// the Fleet tree use. The pill is for inbox group headers and filters, and
// the state at the top of a session's detail: the state's tint, a 1 px
// inset ring in the hue and the word in the text color, since a hue on its
// own tint does not reach AA in Latte. Idle's dot is hollow and finished is
// a check in the neutral color, so no two states differ by color alone. A
// state the service infers from activity rather than reads from the agent
// has a tilde and a dashed edge.
import { Badge, Box, Group, Text } from "@mantine/core";
import { CheckCircle } from "@phosphor-icons/react";
import { stateWord, type State } from "./types";
import "./widgets.css";

/** The shared tone vocabulary, as Canon's status label names it. */
export type StatusTone = "neutral" | "info" | "success" | "warning" | "danger";

/** The tone each state draws in: green working, yellow waiting, red stuck,
 * blue idle; finished is neutral. */
export const stateTone: Record<State, StatusTone> = { working: "success", waiting: "warning", stuck: "danger", idle: "info", finished: "neutral" };

/** toneVar is a tone's CSS color for DOM surfaces; neutral is the quiet
 * control border, so what needs nothing never takes a status hue. */
export function toneVar(tone: StatusTone): string {
  return tone === "neutral" ? "var(--driga-control-border)" : `var(--mantine-color-${tone}-filled)`;
}

/** stateVar is the CSS color a state's dot, rule or bar uses. */
export function stateVar(state: State): string {
  return toneVar(stateTone[state]);
}

/** stateOf reads a session label as the service sends it: a leading ~ marks
 * an inferred one. A label that is no state (dead, unknown, ended) keeps
 * its word and draws neutral. */
export function stateOf(label: string): { state?: State; label: string; inferred: boolean } {
  const inferred = label.startsWith("~");
  const word = label.replace(/^~/, "");
  const state = Object.hasOwn(stateWord, word) ? word as State : undefined;
  return { state, label: word, inferred };
}

/** toneOf is the tone a session label draws in. */
export function toneOf(label: string): StatusTone {
  const { state } = stateOf(label);
  return state ? stateTone[state] : "neutral";
}

export type StatusBadgeProps = {
  /** The state; none draws the label as a neutral state such as dead. */
  state?: State;
  /** dot is the default; pill is the tinted form for kinds and filters. */
  variant?: "dot" | "pill";
  /** The word to show; the state's own by default. */
  label?: string;
  /** An inferred state, read from activity rather than reported by the agent. */
  inferred?: boolean;
  /** The waiting dot may breathe: 2 s, once per widget, off under reduced motion. */
  pulse?: boolean;
  /** A tone of its own, for a kind that is no session state: an inbox group. */
  tone?: StatusTone;
};

export function StatusBadge({ state, variant = "dot", label, inferred, pulse, tone: given }: StatusBadgeProps) {
  const tone = given ?? (state ? stateTone[state] : "neutral");
  const word = label ?? (state ? stateWord[state] : "");
  const name = inferred ? `${word}, inferred` : word;
  const color = toneVar(tone);
  const breathing = pulse && state === "waiting" ? "w-pulse" : undefined;
  const mark = state === "finished"
    ? <CheckCircle size={12} weight="regular" aria-hidden style={{ color: "var(--driga-muted)", flex: "none" }} />
    : <Box aria-hidden className={breathing} style={{ width: 6, height: 6, borderRadius: "50%", flex: "none", background: state === "idle" ? "transparent" : color, boxShadow: state === "idle" ? `inset 0 0 0 1.5px ${color}` : undefined }} />;
  if (variant === "pill") {
    return <Badge variant="light" color={tone === "neutral" ? "gray" : tone} size="md" radius="sm" fw={500} lts="0.04em" leftSection={mark} aria-label={name} title={name}
      styles={{ root: { textTransform: "none", background: `var(--driga-tint-${tone})`, color: "var(--driga-text)", boxShadow: `inset 0 0 0 1px ${color}`, border: inferred ? "1px dashed var(--driga-muted)" : undefined, paddingInline: 6 }, label: { fontSize: "var(--mantine-font-size-xs)" } }}>
      {word}
    </Badge>;
  }
  return <Group role="img" gap={6} wrap="nowrap" aria-label={name} title={name} style={{ display: "inline-flex" }}>
    {mark}
    <Text size="xs" fw={500} lh={1.2} style={{ whiteSpace: "nowrap", borderBottom: inferred ? "1px dashed var(--driga-faint)" : undefined }}>{inferred ? `~${word}` : word}</Text>
  </Group>;
}
