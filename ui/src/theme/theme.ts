/* The Mantine binding for ./tokens.ts. Colors are registered by meaning:
 * `brand` is the accent and success, warning, danger and info are the shared
 * status tones. Each is a virtual color that paints the console's own value in
 * each scheme, so re-hueing Driga is a change to the console palette only. */
import { createTheme, darken, defaultVariantColorsResolver, lighten, luminance, virtualColor, type CSSVariablesResolver, type MantineColorsTuple, type VariantColorsResolver } from "@mantine/core";
import { fonts, mix, surfaces, tones, typeScale, type Scheme, type Tone } from "./tokens";

/** The shade a filled surface paints: the token itself. */
const shade = 6;

/** ramp places a token at the filled shade, with Mantine's lighten and darken
 *  for the steps around it, which hover and light variants use. */
function ramp(hex: string): MantineColorsTuple {
  return Array.from({ length: 10 }, (_, i) => i < shade ? lighten(hex, (shade - i) * 0.14) : i > shade ? darken(hex, (i - shade) * 0.12) : hex) as unknown as MantineColorsTuple;
}

/** Mantine's dark ramp, which dark-scheme surfaces and text read: 0 text,
 *  2 dimmed, 4 borders, 5 hover, 6 control fills, 7 the page. */
const dark: MantineColorsTuple = [
  surfaces.dark.text, surfaces.dark.text, surfaces.dark.muted, surfaces.dark.faint, surfaces.dark.controlBorder,
  surfaces.dark.hover, surfaces.dark.raised, surfaces.dark.ground, darken(surfaces.dark.ground, 0.2), darken(surfaces.dark.ground, 0.4),
];

const toneNames = Object.keys(tones.light) as Tone[];

const colors = Object.fromEntries(toneNames.flatMap(name => [
  [`${name}Light`, ramp(tones.light[name])],
  [`${name}Dark`, ramp(tones.dark[name])],
  [name, virtualColor({ name, light: `${name}Light`, dark: `${name}Dark` })],
]));

export function contrastRatio(a: string, b: string): number {
  const [x, y] = [luminance(a), luminance(b)];
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}

/** The text a filled surface of this color needs: whichever of black and
 *  white measures higher, rather than Mantine's luminance threshold. */
export function filledText(hex: string): string {
  return contrastRatio(hex, "#000000") > contrastRatio(hex, "#ffffff") ? "#000000" : "#ffffff";
}

function contrastVariables(scheme: Scheme) {
  return Object.fromEntries(toneNames.map(name => [`--driga-${name}-contrast`, filledText(tones[scheme][name])]));
}

/** The tones a tint exists for: the status tones, the accent, and a neutral
 *  for what carries no meaning. */
export type TintTone = Tone | "neutral";

/** tint is a tone at 12% over the raised surface: the one tint every tile,
 *  chip, pill, alert and inbox rule uses, so a tinted surface never drops
 *  the text on it below AA. Neutral tints with the faint text role. */
export function tint(scheme: Scheme, tone: TintTone): string {
  const s = surfaces[scheme];
  return mix(tone === "neutral" ? s.faint : tones[scheme][tone], s.raised, 0.12);
}

export const tintTones: readonly TintTone[] = [...toneNames, "neutral"];

function tintVariables(scheme: Scheme) {
  return Object.fromEntries(tintTones.map(tone => [`--driga-tint-${tone}`, tint(scheme, tone)]));
}

function roleVariables(scheme: Scheme) {
  const s = surfaces[scheme];
  return {
    "--mantine-color-body": s.ground, "--mantine-color-text": s.text, "--mantine-color-dimmed": s.muted,
    "--mantine-color-default": s.raised, "--mantine-color-default-hover": s.hover, "--mantine-color-default-border": s.controlBorder,
    "--driga-ground": s.ground, "--driga-raised": s.raised, "--driga-inset": s.inset, "--driga-hover": s.hover,
    "--driga-border": s.border, "--driga-border-soft": s.borderSoft, "--driga-control-border": s.controlBorder,
    "--driga-text": s.text, "--driga-muted": s.muted, "--driga-faint": s.faint,
    ...contrastVariables(scheme),
    ...tintVariables(scheme),
  };
}

export const cssVariables: CSSVariablesResolver = () => ({
  // Invalid fields use the danger tone rather than Mantine's own red.
  variables: { "--mantine-color-error": "var(--mantine-color-danger-filled)" },
  light: roleVariables("light"),
  dark: roleVariables("dark"),
});

/** Filled surfaces in a Driga color take their measured text color. */
const variantColorResolver: VariantColorsResolver = input => {
  const resolved = defaultVariantColorsResolver(input);
  const filled = input.variant === "filled" || input.variant === undefined;
  return filled && toneNames.includes(input.color as Tone) ? { ...resolved, color: `var(--driga-${input.color}-contrast)` } : resolved;
};

export const theme = createTheme({
  primaryColor: "brand",
  primaryShade: shade,
  colors: { dark, ...colors },
  variantColorResolver,
  fontFamily: fonts.sans,
  fontFamilyMonospace: fonts.mono,
  headings: { fontFamily: fonts.sans, fontWeight: "600" },
  fontSizes: { xs: `${typeScale.small / 16}rem`, sm: `${typeScale.small / 16}rem`, md: `${typeScale.body / 16}rem`, lg: `${typeScale.title / 16}rem`, xl: `${typeScale.section / 16}rem` },
  defaultRadius: "sm",
  cursorType: "pointer",
  respectReducedMotion: true,
  other: { labstackTones: { neutral: "gray", info: "info", success: "success", warning: "warning", danger: "danger" } },
  components: {
    // Panels are bordered, not shadowed, on the raised surface.
    Card: { defaultProps: { withBorder: true, radius: "md", padding: "sm" }, styles: { root: { background: "var(--driga-raised)", borderColor: "var(--driga-border)" } } },
    Paper: { styles: { root: { background: "var(--driga-raised)", borderColor: "var(--driga-border)" } } },
    Table: { defaultProps: { highlightOnHover: true, stickyHeader: true, verticalSpacing: 6, horizontalSpacing: "xs", fz: "xs" },
      styles: { thead: { background: "var(--driga-inset)" }, th: { color: "var(--driga-muted)", fontWeight: 500 }, td: { borderColor: "var(--driga-border-soft)" }, tr: { borderColor: "var(--driga-border-soft)" } } },
    Badge: { defaultProps: { variant: "light", radius: "sm" }, styles: { root: { textTransform: "none", fontWeight: 600 } } },
    Button: { defaultProps: { size: "xs" } },
    ActionIcon: { defaultProps: { variant: "subtle", color: "gray" } },
    Tooltip: { defaultProps: { withArrow: true, openDelay: 300 } },
    Modal: { defaultProps: { closeButtonProps: { "aria-label": "Close" } } },
    Drawer: { defaultProps: { closeButtonProps: { "aria-label": "Close" } } },
    // A scrollbar sits beside the content, never over it: Mantine paints
    // its bar inside the viewport, over right-aligned values.
    ScrollArea: { defaultProps: { offsetScrollbars: "present" } },
  },
});
