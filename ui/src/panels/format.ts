import type { Cell, Column } from "./types";

const units = ["B", "KB", "MB", "GB", "TB"];

export function bytes(n: number): string {
  let i = 0;
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
  return `${n >= 10 || i === 0 ? Math.round(n) : n.toFixed(1)} ${units[i]}`;
}

export function duration(ms: number): string {
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h}h ${m % 60}m`;
  return `${Math.floor(h / 24)}d`;
}

export function relative(iso: string, now: number = Date.now()): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "–";
  return `${duration(Math.max(0, now - t))} ago`;
}

/** value formats a number in its column's unit. */
export function value(n: number, unit?: string): string {
  switch (unit) {
    case "bytes": return bytes(n);
    case "duration": return duration(n);
    case "percent": return `${Math.round(n)}%`;
    default: return Number.isInteger(n) ? n.toLocaleString() : n.toFixed(2);
  }
}

/** cell formats any frame cell for a table. Missing values show a dash, never zero. */
export function cell(c: Cell, column: Column): string {
  if (c === null || c === undefined || c === "") return "–";
  if (column.type === "number" && typeof c === "number") return value(c, column.unit);
  if (column.type === "time" && typeof c === "string") return relative(c);
  if (column.type === "bool") return c ? "yes" : "no";
  return String(c);
}

/** label turns a column name into a header. */
export function label(name: string): string {
  const words = name.replace(/_bytes$|_at$/, "").replaceAll("_", " ");
  return words.charAt(0).toUpperCase() + words.slice(1);
}
