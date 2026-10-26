// The fictional fleet for the machine card: studio, build-01 and laptop-02.
import { ago, GB } from "./KpiTile-fixtures";
import type { Machine } from "./MachineCard";
import { minutes } from "./TrackerBar-fixtures";

export const studio: Machine = { name: "studio", kind: "desktop", local: true, reachable: true, lastSeen: ago(0.07), memUsed: 25 * GB, memTotal: 36 * GB, sessions: { working: 5, waiting: 1, stuck: 0, idle: 2 }, strip: minutes(31, { waits: 2 }), leftover: { trees: 2, bytes: 1.8 * GB } };

export const build: Machine = { name: "build-01", kind: "server", tls: true, reachable: true, lastSeen: ago(0.1), memUsed: 30.1 * GB, memTotal: 32 * GB, sessions: { working: 2, waiting: 0, stuck: 1, idle: 0 }, strip: minutes(32, { waits: 1, stuck: true }), leftover: { trees: 3, bytes: 1.8 * GB } };

export const laptop: Machine = { name: "laptop-02", kind: "laptop", tls: true, reachable: false, lastSeen: ago(4), memUsed: 11 * GB, memTotal: 16 * GB, sessions: { working: 0, waiting: 0, stuck: 0, idle: 1 }, strip: minutes(33, { quiet: true }), leftover: { trees: 0, bytes: 0 } };

export const laptopLost: Machine = { ...laptop, lastSeen: ago(38), sessions: { working: 0, waiting: 0, stuck: 0, idle: 0 } };

export const machines: Machine[] = [studio, build, laptop];
