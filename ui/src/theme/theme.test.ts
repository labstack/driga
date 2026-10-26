import { describe, expect, it } from "vitest";
import { contrastRatio, filledText, tint, tintTones } from "./theme";
import { surfaces, tones, type Scheme } from "./tokens";

const schemes: Scheme[] = ["light", "dark"];

describe("theme contrast", () => {
  for (const scheme of schemes) {
    const s = surfaces[scheme];
    it(`${scheme}: text and muted text pass AA on ground, raised and hover`, () => {
      for (const bg of [s.ground, s.raised, s.hover]) {
        expect(contrastRatio(s.text, bg), `text on ${bg}`).toBeGreaterThanOrEqual(4.5);
        expect(contrastRatio(s.muted, bg), `muted on ${bg}`).toBeGreaterThanOrEqual(4.5);
      }
    });
    it(`${scheme}: control borders reach 3:1 on raised`, () => {
      expect(contrastRatio(s.controlBorder, s.raised)).toBeGreaterThanOrEqual(3);
    });
    it(`${scheme}: every filled tone carries AA text`, () => {
      for (const [name, hex] of Object.entries(tones[scheme])) {
        expect(contrastRatio(filledText(hex), hex), name).toBeGreaterThanOrEqual(4.5);
      }
    });
    it(`${scheme}: tones read as text on every surface`, () => {
      for (const bg of [s.ground, s.raised, s.inset]) {
        for (const [name, hex] of Object.entries(tones[scheme])) {
          expect(contrastRatio(hex, bg), `${name} on ${bg}`).toBeGreaterThanOrEqual(4.5);
        }
      }
    });
    // A tinted tile or pill keeps its text and muted text at AA; the dot and
    // ring drawn in the tone on its own tint reach the 3:1 non-text bar. The
    // word on a pill is in the text color, since a Latte hue on its tint
    // (danger: 4.35:1) does not reach AA.
    it(`${scheme}: text and muted text pass AA on every tint, and the tone draws at 3:1 on it`, () => {
      for (const tone of tintTones) {
        const bg = tint(scheme, tone);
        expect(contrastRatio(s.text, bg), `text on ${tone} tint`).toBeGreaterThanOrEqual(4.5);
        expect(contrastRatio(s.muted, bg), `muted on ${tone} tint`).toBeGreaterThanOrEqual(4.5);
        if (tone !== "neutral") expect(contrastRatio(tones[scheme][tone], bg), `${tone} on its tint`).toBeGreaterThanOrEqual(3);
      }
    });
  }
});
