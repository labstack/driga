// What the app keeps between refreshes until the service keeps it: an hour
// of each attention count, a reading a minute, and each machine's last 30
// polls of its worst state. Both start empty when the page opens and fill
// as it refreshes; the header's deltas and the Fleet's strips read them.
import { useEffect, useRef, useState } from "react";
import type { FleetData } from "../fleet/FleetTypes";
import { attentionKinds, machinesOf, nothingKept, pollOf, type AttentionKind, type Kept, type Poll } from "./Header";

const hour = 60 * 60_000;
const maxPolls = 30;

type Sample = { at: number; counts: Record<AttentionKind, number> };

/** trendOf is an hour of readings, one a minute, from samples taken at any
 * rate: each minute carries the last reading at or before it. */
export function trendOf(samples: Sample[], kind: AttentionKind, now: number): number[] {
  if (!samples.length) return [];
  const first = Math.max(samples[0].at, now - hour);
  const minutes = Math.max(1, Math.floor((now - first) / 60_000) + 1);
  const out: number[] = [];
  let i = 0;
  for (let m = 0; m < minutes; m++) {
    const at = first + m * 60_000;
    while (i + 1 < samples.length && samples[i + 1].at <= at) i++;
    out.push(samples[i].counts[kind]);
  }
  return out;
}

/** useHistory samples the counts and the fleet each time they change and
 * returns what has been kept so far. */
export function useHistory(counts: Record<AttentionKind, number> | null, fleet: FleetData | null): Kept {
  const samples = useRef<Sample[]>([]);
  const polls = useRef<Record<string, Poll[]>>({});
  const [kept, setKept] = useState<Kept>(nothingKept);
  useEffect(() => {
    const now = Date.now();
    if (counts) {
      samples.current.push({ at: now, counts });
      samples.current = samples.current.filter(s => s.at >= now - hour);
    }
    if (fleet) {
      for (const m of machinesOf(fleet, {})) polls.current[m.id] = [...(polls.current[m.id] ?? []), pollOf(m)].slice(-maxPolls);
    }
    setKept({
      attention: Object.fromEntries(attentionKinds.map(k => [k, trendOf(samples.current, k, now)])) as Kept["attention"],
      polls: { ...polls.current },
    });
  }, [counts, fleet]);
  return kept;
}
