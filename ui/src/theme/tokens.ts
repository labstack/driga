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

/** Surface and text roles, named by use (see Canon's visual standard). */
export const surfaces = {
  light: {
    ground: flavor.light.mantle, raised: flavor.light.base, inset: flavor.light.crust, hover: flavor.light.surface0,
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

/** Six fixed categorical slots for chart series, in order. None is a state
 *  hue, so a series is never read as a status; further series are muted. */
export const series = {
  light: [flavor.light.lavender, flavor.light.peach, flavor.light.teal, flavor.light.pink, flavor.light.sky, flavor.light.flamingo],
  dark: [flavor.dark.lavender, flavor.dark.peach, flavor.dark.teal, flavor.dark.pink, flavor.dark.sky, flavor.dark.flamingo],
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
