// The board's fixtures: every widget's own, gathered, in a busy fleet and
// a calm one.
import * as gauges from "./BarGauge-fixtures";
import * as lists from "./BarList-fixtures";
import * as splits from "./CategoryBar-fixtures";
import { chart } from "./ChartCard-fixtures";
import * as donuts from "./Donut-fixtures";
import type { GalleryProps } from "./Gallery";
import { activity } from "./Heatmap-fixtures";
import * as inbox from "./InboxItem-fixtures";
import * as kpis from "./KpiTile-fixtures";
import * as fleet from "./MachineCard-fixtures";
import * as cards from "./SessionCard-fixtures";
import { swimlanes } from "./SessionSwimlanes-fixtures";
import * as strips from "./TrackerBar-fixtures";
import * as rings from "./RingGauge-fixtures";

const checkedAt = new Date(kpis.now - 20_000).toISOString();

export const busy: GalleryProps = {
  tiles: kpis.tiles,
  chart: chart("6h"),
  donut: donuts.attribution,
  machines: fleet.machines,
  inbox: inbox.entries,
  sessions: cards.sessions,
  heatmap: activity,
  swimlanes,
  ranking: lists.memoryByProject,
  split: splits.memorySplit,
  tracker: { cells: strips.turns, label: "Last 90 turns of the busiest session", from: "turn 1", to: "turn 90" },
  rings: [rings.memory, rings.disk],
  gauge: gauges.swap,
  checkedAt,
};

export const calm: GalleryProps = {
  ...busy,
  tiles: [{ ...kpis.needYou, value: 0, delta: { change: -3, window: "1h" } }, { ...kpis.toReview, value: 0, delta: undefined }, kpis.stuck, kpis.memory, kpis.quietLeftover, { ...kpis.reclaimable, value: 0, delta: undefined }],
  inbox: [],
  sessions: [cards.working, cards.idle],
  machines: [{ ...fleet.studio, sessions: { working: 2, waiting: 0, stuck: 0, idle: 1 }, strip: strips.minutes(51), leftover: { trees: 0, bytes: 0 } }, { ...fleet.build, memUsed: 14 * kpis.GB, sessions: { working: 1, waiting: 0, stuck: 0, idle: 0 }, strip: strips.minutes(52), leftover: { trees: 0, bytes: 0 } }, fleet.laptop],
};
