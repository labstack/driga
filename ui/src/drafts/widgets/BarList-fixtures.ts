// Fictional rankings for the bar list.
import { bytes } from "../../panels/format";
import type { BarListProps } from "./BarList";
import { GB } from "./KpiTile-fixtures";

const MB = 1024 ** 2;

export const memoryByProject: BarListProps = {
  label: "Memory by project", format: bytes,
  items: [
    { id: "atlas", label: "atlas", value: 6.2 * GB, sub: "4 sessions" },
    { id: "harbor", label: "harbor", value: 3.9 * GB, sub: "2 sessions" },
    { id: "beacon", label: "beacon", value: 1.7 * GB, sub: "2 sessions" },
    { id: "sandbox", label: "sandbox", value: 640 * MB, sub: "1 session" },
  ],
};

export const tokensBySession: BarListProps = {
  label: "Tokens today by session", format: n => `${(n / 1000).toFixed(0)}k`, ranked: true,
  items: [
    { id: "s3", label: "Rewrite the dashboard layout tests", value: 412_000, sub: "agy · atlas" },
    { id: "s1", label: "Fix the race in the rate limiter", value: 268_000, sub: "codex · atlas" },
    { id: "s2", label: "Add retries to the harbor client", value: 171_000, sub: "claude · harbor" },
    { id: "s5", label: "Migrate the index store to WAL", value: 96_000, sub: "copilot · beacon" },
    { id: "s4", label: "Draft the release notes", value: 12_000, sub: "gemini · beacon" },
  ],
};
