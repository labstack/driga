// Fictional readings for the KPI tiles, and the clock and random walk the
// other widget fixtures share. The fleet was observed at `now`, the same
// instant the catalog pins Date.now to, so "27m ago" reads the same every run.
import { Broom, Eye, HandWaving, Hourglass, Memory, TreeStructure } from "@phosphor-icons/react";
import { storyNow } from "../fixtures/fragments";
import type { KpiTileProps } from "./KpiTile";

export const now = storyNow;

export const GB = 1024 ** 3;

/** ago is an ISO time some minutes before now. */
export function ago(minutes: number): string {
  return new Date(now - minutes * 60_000).toISOString();
}

/** rng is a seeded generator, so a story draws the same picture every run. */
export function rng(seed: number): () => number {
  let s = seed >>> 0;
  return () => { s = (Math.imul(s, 1664525) + 1013904223) >>> 0; return s / 4294967296; };
}

/** walk is n readings that wander from start by up to step, within bounds. */
export function walk(seed: number, n: number, start: number, step: number, min: number, max: number): number[] {
  const r = rng(seed);
  const out: number[] = [];
  let v = start;
  for (let i = 0; i < n; i++) { v = Math.min(max, Math.max(min, v + (r() - 0.5) * 2 * step)); out.push(v); }
  return out;
}

export const needYou: KpiTileProps = { label: "Need you", icon: HandWaving, value: 3, unit: "count", attention: "waiting", delta: { change: 2, window: "1h" }, direction: "down-good", trend: walk(1, 60, 1, 1, 0, 5).map(Math.round), description: "Sessions waiting on an answer or an approval" };

export const toReview: KpiTileProps = { label: "To review", icon: Eye, value: 10, unit: "count", attention: "review", delta: { change: 4, window: "1h" }, trend: walk(2, 60, 6, 1, 0, 12).map(Math.round), description: "Finished turns you have not looked at" };

export const stuck: KpiTileProps = { label: "Stuck", icon: Hourglass, value: 0, unit: "count", attention: "stuck", delta: { change: -1, window: "1h" }, direction: "down-good", trend: walk(3, 60, 1, 0.6, 0, 2).map(Math.round), description: "Sessions quiet longer than they usually are" };

export const stuckOne: KpiTileProps = { ...stuck, value: 1, delta: { change: 1, window: "1h" } };

export const memory: KpiTileProps = { label: "Memory · studio", icon: Memory, value: 25 * GB, unit: "bytes", total: 36 * GB, delta: { change: -1.8 * GB, window: "1h" }, direction: "down-good", trend: walk(4, 60, 27 * GB, 0.4 * GB, 20 * GB, 30 * GB), description: "Used memory on this machine" };

export const leftover: KpiTileProps = { label: "Leftover", icon: TreeStructure, value: 2, unit: "count", caption: "1.8 GB", detail: "process trees", delta: { change: 0, window: "1h" }, direction: "down-good", description: "Process trees whose session ended" };

export const reclaimable: KpiTileProps = { label: "Reclaimable", icon: Broom, value: 6.1 * GB, unit: "bytes", delta: { change: 1.2 * GB, window: "1h" }, direction: "down-good", trend: walk(5, 60, 5 * GB, 0.3 * GB, 3 * GB, 7 * GB), description: "Leftover trees and stale worktrees Driga can remove" };

export const quietLeftover: KpiTileProps = { ...leftover, value: 0, caption: undefined, delta: undefined };

export const tiles: KpiTileProps[] = [needYou, toReview, stuck, memory, leftover, reclaimable];
