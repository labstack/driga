// Fictional splits for the category bar.
import { bytes } from "../panels/format";
import type { CategoryBarProps } from "./CategoryBar";
import { GB } from "./KpiTile-fixtures";

export const memorySplit: CategoryBarProps = {
  label: "Memory on studio by owner",
  segments: [{ label: "Sessions", value: 10.4 * GB }, { label: "MCP servers", value: 3.1 * GB }, { label: "Agents without a session", value: 1.2 * GB }, { label: "Leftovers", value: 1.8 * GB }, { label: "Everything else", value: 8.5 * GB, color: "brand" }],
  total: 36 * GB, marker: { value: 32.4 * GB, label: "90%" }, format: bytes,
};

export const sessionsByState: CategoryBarProps = {
  label: "Sessions by state",
  segments: [{ label: "working", value: 7, color: "working" }, { label: "waiting", value: 1, color: "waiting" }, { label: "stuck", value: 1, color: "stuck" }, { label: "idle", value: 3, color: "idle" }],
  format: n => `${n}`,
};
