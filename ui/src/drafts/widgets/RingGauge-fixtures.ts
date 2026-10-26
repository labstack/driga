// Fictional pressures for the ring gauge.
import { bytes } from "../../panels/format";
import { GB } from "./KpiTile-fixtures";
import type { RingGaugeProps } from "./RingGauge";

export const memory: RingGaugeProps = { value: 25 * GB, max: 36 * GB, format: bytes, label: "Memory" };
export const memoryHot: RingGaugeProps = { value: 30.1 * GB, max: 32 * GB, format: bytes, label: "Memory" };
export const disk: RingGaugeProps = { value: 412 * GB, max: 994 * GB, format: bytes, label: "Disk" };
export const diskWarm: RingGaugeProps = { value: 760 * GB, max: 994 * GB, format: bytes, label: "Disk", caption: "760 GB · 234 GB free" };
