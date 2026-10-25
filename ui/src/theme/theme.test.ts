import { describe, expect, it } from "vitest";
import { contrastRatio, filledText } from "./theme";
import { surfaces, tones, type Scheme } from "./tokens";

const schemes: Scheme[] = ["light", "dark"];

describe("theme contrast", () => {
  for (const scheme of schemes) {
    const s = surfaces[scheme];
    it(`${scheme}: text and muted text pass AA on ground and raised`, () => {
      for (const bg of [s.ground, s.raised]) {
        expect(contrastRatio(s.text, bg)).toBeGreaterThanOrEqual(4.5);
        expect(contrastRatio(s.muted, bg)).toBeGreaterThanOrEqual(4.5);
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
    it(`${scheme}: tones read as text on raised`, () => {
      for (const [name, hex] of Object.entries(tones[scheme])) {
        expect(contrastRatio(hex, s.raised), name).toBeGreaterThanOrEqual(4.5);
      }
    });
  }
});
