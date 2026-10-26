/* The agent families' colors, the one module in the widgets that holds
 * literal values: each family's categorical hue from Catppuccin's accents.
 * Everything else a widget paints comes from the theme's variables, and
 * charts take their resolved colors from panels/charts' Theme. */
import { alpha, darken } from "@mantine/core";
import palette from "../theme/palette.json";
import type { Scheme } from "../theme/tokens";
import { agentOf, type Agent } from "./types";

const accents = { light: palette.latte, dark: palette.mocha } as const;

/** Each agent family's categorical hue: peach, teal, sapphire, sky, pink.
 * None is a state color, so a chip is never read as a status. */
export const agentHue: Record<Scheme, Record<Agent, string>> = {
  light: { claude: accents.light.peach, codex: accents.light.teal, gemini: accents.light.sapphire, copilot: accents.light.sky, agy: accents.light.pink },
  dark: { claude: accents.dark.peach, codex: accents.dark.teal, gemini: accents.dark.sapphire, copilot: accents.dark.sky, agy: accents.dark.pink },
};

/** A chip's tint, its inset ring and the color its glyph draws in. Latte's
 * accents are too light to draw a glyph on a tint, so there they darken. A
 * family Driga does not know takes the neutral roles. */
export function agentColors(scheme: Scheme, name: string | undefined): { tint: string; ring: string; ink: string } {
  const agent = agentOf(name);
  if (!agent) return { tint: "var(--driga-tint-neutral)", ring: "var(--driga-border)", ink: "var(--driga-muted)" };
  const hue = agentHue[scheme][agent];
  return { tint: alpha(hue, 0.16), ring: alpha(hue, 0.4), ink: scheme === "dark" ? hue : darken(hue, 0.3) };
}
