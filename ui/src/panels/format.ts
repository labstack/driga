// Formatters for panel values, built once with Intl as Canon asks. Missing
// values show a dash, never zero.
import type { Cell, Column } from "./types";

const units = ["B", "KB", "MB", "GB", "TB"];
const whole = new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 });
const tenths = new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 });
const hundredths = new Intl.NumberFormat(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
const usd = new Intl.NumberFormat(undefined, { style: "currency", currency: "USD", maximumFractionDigits: 2 });
const usdWhole = new Intl.NumberFormat(undefined, { style: "currency", currency: "USD", maximumFractionDigits: 0 });
const utc = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "medium", timeZone: "UTC" });

export function bytes(n: number): string {
  let i = 0;
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
  return `${n >= 10 || i === 0 ? whole.format(n) : tenths.format(n)} ${units[i]}`;
}

/** duration reads the two largest units: 45s, 12m, 3h 5m, 2d 4h. */
export function duration(ms: number): string {
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ${m % 60}m`;
  return `${Math.floor(h / 24)}d ${h % 24}h`;
}

export function relative(iso: string, now: number = Date.now()): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "–";
  return `${duration(Math.max(0, now - t))} ago`;
}

/** exact is a time's full UTC value, for hover and focus. */
export function exact(iso: string): string {
  const t = Date.parse(iso);
  return Number.isNaN(t) ? iso : `${utc.format(t)} UTC`;
}

/** value formats a number in its column's unit. */
export function value(n: number, unit?: string): string {
  switch (unit) {
    case "bytes": return bytes(n);
    case "duration": return duration(n);
    case "percent": return `${whole.format(n)}%`;
    case "usd": return n < 10 ? usd.format(n) : usdWhole.format(n);
    default: return Number.isInteger(n) ? whole.format(n) : hundredths.format(n);
  }
}

/** cell formats any frame cell for a table. */
export function cell(c: Cell, column: Column): string {
  if (c === null || c === undefined || c === "") return "–";
  if (column.type === "number" && typeof c === "number") return value(c, column.unit);
  if (column.type === "time" && typeof c === "string") return relative(c);
  if (column.type === "bool") return c ? "yes" : "no";
  return String(c);
}

/** plural counts a thing: 1 file, 2 files, 3 processes. */
export function plural(n: number, word: string): string {
  return `${whole.format(n)} ${word}${n === 1 ? "" : word.endsWith("s") ? "es" : "s"}`;
}

/** label turns a column name into a header. */
export function label(name: string): string {
  const words = name.replace(/_bytes$|_at$/, "").replaceAll("_", " ");
  return words.charAt(0).toUpperCase() + words.slice(1);
}
