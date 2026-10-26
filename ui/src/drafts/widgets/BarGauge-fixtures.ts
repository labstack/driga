// Fictional pressures for the bar gauge.
import { bytes } from "../../panels/format";
import type { BarGaugeProps } from "./BarGauge";
import { GB } from "./KpiTile-fixtures";

export const calm: BarGaugeProps = { value: 25 * GB, max: 36 * GB, format: bytes, label: "Memory on studio" };
export const warm: BarGaugeProps = { value: 12.4 * GB, max: 16 * GB, format: bytes, label: "Memory on laptop-02" };
export const hot: BarGaugeProps = { value: 30.1 * GB, max: 32 * GB, format: bytes, label: "Memory on build-01" };
export const disk: BarGaugeProps = { value: 412 * GB, max: 994 * GB, format: bytes, label: "Disk on studio" };
export const swap: BarGaugeProps = { value: 6 * GB, max: 8 * GB, format: bytes, label: "Swap on build-01" };
