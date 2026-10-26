/* Driga's design tokens.
 *
 * Every value comes from palette.json, which the Go console theme writes
 * (internal/view.ThemeJSON), so the app, its MCP App cards and the TUI draw
 * in the same Catppuccin colors: Latte for light, Mocha for dark. Change a
 * color in internal/view, not here.
 *
 * The accent is Mauve. Green, yellow, red and blue already mean working,
 * waiting on you, stuck and idle on every surface Driga draws, so the hue
 * free to mean "you can click this" is the one no state uses. */
import palette from "./palette.json";

export type Scheme = "light" | "dark";

const flavor = { light: palette.latte, dark: palette.mocha } as const;
const roles = { light: palette.light, dark: palette.dark } as const;

/** mix blends two hex colors in sRGB: `weight` of a over b, so
 *  mix(tone, raised, 0.12) is a 12% tint of the tone on the raised surface. */
export function mix(a: string, b: string, weight: number): string {
  const channel = (hex: string, i: number) => parseInt(hex.slice(1 + i * 2, 3 + i * 2), 16);
  return "#" + [0, 1, 2].map(i => Math.round(channel(a, i) * weight + channel(b, i) * (1 - weight)).toString(16).padStart(2, "0")).join("");
}

/** Surface and text roles, named by use (see Canon's visual standard). The
 *  light hover is surface0 mixed half with the base, so muted text keeps AA
 *  on a hovered row. */
export const surfaces = {
  light: {
    ground: flavor.light.mantle, raised: flavor.light.base, inset: flavor.light.crust, hover: mix(flavor.light.surface0, flavor.light.base, 0.5),
    border: flavor.light.surface0, borderSoft: flavor.light.crust, controlBorder: flavor.light.overlay2,
    text: roles.light.fg, muted: roles.light.dim, faint: flavor.light.overlay0,
  },
  dark: {
    ground: flavor.dark.crust, raised: flavor.dark.base, inset: flavor.dark.mantle, hover: flavor.dark.surface0,
    border: flavor.dark.surface0, borderSoft: roles.dark["flow-line"], controlBorder: flavor.dark.overlay0,
    text: roles.dark.fg, muted: roles.dark.dim, faint: flavor.dark.overlay1,
  },
} as const;

/** The interactive accent and the shared status tones, per scheme. The text
 *  roles are the console's, darkened in light only as far as AA needs. */
export const tones = {
  light: { brand: roles.light.accent, success: roles.light.working, warning: roles.light.waiting, danger: roles.light.stalled, info: roles.light.idle },
  dark: { brand: roles.dark.accent, success: roles.dark.working, warning: roles.dark.waiting, danger: roles.dark.stalled, info: roles.dark.idle },
} as const;

export type Tone = keyof typeof tones.light;

/** Six fixed categorical slots for chart series, in order: the Catppuccin
 *  peach, teal, pink, sky, lavender and flamingo hues, re-stepped so every
 *  slot clears 3:1 on its surface and adjacent slots stay apart under color
 *  blindness (the dataviz palette validator passes both sets). None is a
 *  state hue or the accent, so a series is never read as a status or a
 *  control; further series are muted. */
export const series = {
  light: ["#b64c1a", "#1c989f", "#a40987", "#0781b4", "#3d37e6", "#b75a5b"],
  dark: ["#c97338", "#049b8c", "#b1389a", "#069eb1", "#6e74f5", "#c97276"],
} as const;

/** Text sizes in pixels. micro is for chart axes only; nothing a person reads
 *  is smaller than small. */
export const typeScale = { micro: 11, small: 12, body: 14, title: 16, section: 22, page: 28 } as const;

/** One sans for everything a person reads; mono only for data. The app serves
 *  IBM Plex; MCP App hosts grant no fonts, so cards fall back to the system's. */
export const fonts = {
  sans: '"IBM Plex Sans", system-ui, -apple-system, "Segoe UI", sans-serif',
  mono: '"IBM Plex Mono", ui-monospace, "SF Mono", Menlo, Consolas, monospace',
} as const;
