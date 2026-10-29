import { Broom, ChartBar, Cpu, Eye, Gauge, HandWaving, HardDrives, Hourglass, Memory, Stack, TreeStructure, type Icon } from "@phosphor-icons/react";
import type { Panel } from "../panels/types";
import { value as format } from "../panels/format";
import type { Direction } from "../widgets/KpiTile";
import type { AttentionKind, HeaderTarget } from "./Header";

export type TileBehavior = { attention?: AttentionKind; icon: Icon; target?: HeaderTarget; direction: Direction; sublabel?: "local-machine"; countNouns?: Record<string, readonly [string, string]> };
const neutral: TileBehavior = { icon: ChartBar, direction: "neutral" };
const behaviors: Record<string, TileBehavior> = {
  "summary:sum:need_you": { icon: HandWaving, attention: "waiting", target: "inbox", direction: "down-good" },
  "summary:sum:to_review": { icon: Eye, attention: "review", target: "inbox", direction: "neutral" },
  "summary:sum:attention_stuck": { icon: Hourglass, attention: "stuck", target: "stuck", direction: "down-good" },
  "summary:sum:leftover_bytes": { icon: TreeStructure, target: "leftover", direction: "down-good", countNouns: { leftover: ["tree", "trees"] } },
  "summary:sum:reclaim_bytes": { icon: Broom, target: "reclaim", direction: "down-good" },
  "local_activity:max:mem_used_bytes": { icon: Memory, target: "memory", direction: "down-good", sublabel: "local-machine" },
  "local_activity:max:cpu_percent": { icon: Cpu, direction: "down-good", sublabel: "local-machine" },
  "local_activity:max:disk_used_bytes": { icon: HardDrives, direction: "down-good", sublabel: "local-machine" },
  "local_activity:max:load1": { icon: Gauge, direction: "down-good", sublabel: "local-machine" },
  "local_activity:max:swap_used_bytes": { icon: Memory, direction: "down-good", sublabel: "local-machine" },
  "local_activity:max:events": { icon: ChartBar, direction: "neutral", sublabel: "local-machine" },
  "summary:sum:machines_reachable": { icon: Stack, direction: "up-good" },
};

/** Behavior follows the metric, including when Chat gives it a different id. */
export function behaviorOf(panel: Panel): TileBehavior {
  return behaviors[`${panel.source}:${panel.measure ?? "count"}`] ?? neutral;
}

/** Count wording follows the caption column, independent of aggregation or panel name. */
export function captionOf(panel: Panel, count: number): string {
  const column = panel.caption?.split(":").at(-1);
  const nouns = column ? behaviorOf(panel).countNouns?.[column] : undefined;
  return `${format(count)}${nouns ? ` ${nouns[count === 1 ? 0 : 1]}` : ""}`;
}
