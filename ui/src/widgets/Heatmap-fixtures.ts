// A fictional day of tool calls per project and hour for the heatmap.
import type { HeatmapProps } from "./Heatmap";
import { rng } from "./KpiTile-fixtures";

const projects = ["atlas", "harbor", "beacon", "sandbox"];
const hours = Array.from({ length: 24 }, (_, h) => `${String(h).padStart(2, "0")}:00`);

/** Daytime is busy, nights are quiet, and atlas had an evening push. */
function day(seed: number, lift: (h: number) => number): number[] {
  const r = rng(seed);
  return hours.map((_, h) => {
    const awake = h >= 8 && h <= 19 ? 1 : h >= 20 && h <= 23 ? 0.4 : 0.05;
    return Math.round(r() * 40 * awake * lift(h));
  });
}

export const activity: HeatmapProps = {
  label: "Tool calls per project and hour, last 24 hours", unit: "tool calls", format: n => `${n}`,
  rows: projects, cols: hours,
  values: [day(41, h => h >= 20 ? 3 : 1.4), day(42, () => 1), day(43, h => h < 12 ? 1.2 : 0.5), day(44, () => 0.4)],
};
