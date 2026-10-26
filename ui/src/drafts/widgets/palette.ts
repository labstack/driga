/* The widget drafts' colors, the one module in the drafts that holds literal
 * values. Everything a DOM widget paints comes from the theme's variables;
 * charts draw into a canvas that cannot read them, so they take resolved
 * colors from here per scheme.
 *
 * Series slots are the validated chart palette from the online research
 * (section 5c): the same hue families as Catppuccin, re-stepped so every
 * slot clears 3:1 on its surface and neighbours stay apart under color
 * blindness. Catppuccin's own accents stay for chrome, badges and the agent
 * tints. Green, yellow, red and blue keep their meaning everywhere. */
import { alpha, darken } from "@mantine/core";
import type { Theme } from "../../panels/charts";
import palette from "../../theme/palette.json";
import { fonts, surfaces, tones, typeScale, type Scheme, type Tone } from "../../theme/tokens";
import type { Agent, State } from "./types";

/** Seven categorical slots in fixed order: blue, orange, teal, yellow, pink,
 * green, violet. A series keeps its slot; further series are muted. */
export const chartSeries: Record<Scheme, readonly string[]> = {
  dark: ["#3f82e8", "#d8702a", "#0f9a8c", "#c98500", "#c7459c", "#2f9c38", "#8f62e8"],
  light: ["#1e66f5", "#d9530a", "#15a37f", "#c98500", "#c9479e", "#2f8f22", "#8839ef"],
};

const accents = { light: palette.latte, dark: palette.mocha } as const;

/** Each agent family's categorical hue: peach, teal, sapphire, sky, pink.
 * None is a state color, so a chip is never read as a status. */
export const agentHue: Record<Scheme, Record<Agent, string>> = {
  light: { claude: accents.light.peach, codex: accents.light.teal, gemini: accents.light.sapphire, copilot: accents.light.sky, agy: accents.light.pink },
  dark: { claude: accents.dark.peach, codex: accents.dark.teal, gemini: accents.dark.sapphire, copilot: accents.dark.sky, agy: accents.dark.pink },
};

/** A chip's tint, its inset ring and the color its glyph draws in. Latte's
 * accents are too light to draw a glyph on a tint, so there they darken. */
export function agentColors(scheme: Scheme, agent: Agent): { tint: string; ring: string; ink: string } {
  const hue = agentHue[scheme][agent];
  return { tint: alpha(hue, 0.16), ring: alpha(hue, 0.4), ink: scheme === "dark" ? hue : darken(hue, 0.3) };
}

/** The tone each state paints with in the DOM, through Mantine's variables. */
export const stateTone: Record<State, Tone> = { working: "success", waiting: "warning", stuck: "danger", idle: "info", finished: "info" };

/** stateVar is the CSS variable a state's dot, rule or bar uses. */
export function stateVar(state: State): string {
  return `var(--mantine-color-${stateTone[state]}-filled)`;
}

/** The same four meanings as resolved values, for canvas charts. */
export function stateColors(scheme: Scheme): Record<State, string> {
  const t = tones[scheme];
  return { working: t.success, waiting: t.warning, stuck: t.danger, idle: t.info, finished: t.info };
}

/** WidgetTheme adds to the app's chart theme what the drafts' charts need:
 * the scheme, the inset surface tooltips sit on, the accent, the state
 * colors and the validated series slots. */
export type WidgetTheme = Theme & { scheme: Scheme; inset: string; brand: string; states: Record<State, string>; palette: readonly string[]; mono: string; sans: string; micro: number; small: number };

export function widgetTheme(t: Theme): WidgetTheme {
  const scheme: Scheme = t.surface === surfaces.dark.raised ? "dark" : "light";
  return { ...t, scheme, inset: surfaces[scheme].inset, brand: tones[scheme].brand, states: stateColors(scheme), palette: chartSeries[scheme], mono: fonts.mono, sans: fonts.sans, micro: typeScale.micro, small: typeScale.small };
}

/** fade is a color at an opacity, for area fills and tints on canvas. */
export function fade(color: string, opacity: number): string {
  return alpha(color, opacity);
}
