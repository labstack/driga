// A fictional attribution of fleet memory for the donut.
import { bytes } from "../../panels/format";
import type { DonutProps } from "./Donut";
import { GB } from "./KpiTile-fixtures";

export const attribution: DonutProps = {
  label: "Fleet memory by bucket", format: bytes,
  slices: [{ name: "Sessions", value: 18.3 * GB }, { name: "MCP servers", value: 5.6 * GB }, { name: "Agents without a session", value: 2.1 * GB }, { name: "Leftovers", value: 3.6 * GB }],
  centre: { value: "29.6 GB", caption: "agents' memory" },
};

export const empty: DonutProps = { label: "Fleet memory by bucket", format: bytes, slices: [], centre: { value: "0 B", caption: "no agents" } };
