// The fictional fleet for the machine card: studio, build-01 and laptop-02.
import { ago, GB } from "./KpiTile-fixtures";
import type { Machine } from "./MachineCard";
import { minutes } from "./TrackerBar-fixtures";

const strip = (name: string, seed: number, options: Parameters<typeof minutes>[1]) => ({ cells: minutes(seed, options), from: "90m", to: "now", label: `${name}, last 90 minutes` });

export const studio: Machine = { id: "studio-id", name: "studio", kind: "desktop", local: true, reachable: true, lastSeen: ago(0.07), memUsed: 25 * GB, memTotal: 36 * GB, sessions: { working: 5, waiting: 1, stuck: 0, idle: 2 }, strip: strip("studio", 31, { waits: 2 }), leftover: { trees: 2, bytes: 1.8 * GB } };

export const build: Machine = { id: "build-id", name: "build-01", kind: "server", tls: true, reachable: true, lastSeen: ago(0.1), memUsed: 30.1 * GB, memTotal: 32 * GB, sessions: { working: 2, waiting: 0, stuck: 1, idle: 0 }, strip: strip("build-01", 32, { waits: 1, stuck: true }), leftover: { trees: 3, bytes: 1.8 * GB } };

export const laptop: Machine = { id: "laptop-id", name: "laptop-02", kind: "laptop", tls: true, reachable: false, lastSeen: ago(4), memUsed: 11 * GB, memTotal: 16 * GB, sessions: { working: 0, waiting: 0, stuck: 0, idle: 1 }, strip: strip("laptop-02", 33, { quiet: true, offline: 4 }), leftover: { trees: 0, bytes: 0 } };

export const laptopLost: Machine = { ...laptop, lastSeen: ago(38), sessions: { working: 0, waiting: 0, stuck: 0, idle: 0 } };

/** A machine Driga has only just met: no history for a strip yet. */
export const fresh: Machine = { ...studio, id: "lab-id", name: "lab-01", local: false, strip: undefined, sessions: { working: 1, waiting: 0, stuck: 0, idle: 0 }, leftover: { trees: 0, bytes: 0 } };

export const machines: Machine[] = [studio, build, laptop];
