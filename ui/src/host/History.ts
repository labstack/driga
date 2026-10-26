// A pure read of the service's durable history. Reloading the page changes
// nothing; the browser records no samples and invents no missing readings.
import type { Fragment } from "../panels/types";
import { attentionKinds, nothingKept, type Kept } from "./Header";

export function historyOf(fragment: Fragment | null): Kept {
  if (!fragment) return nothingKept;
  const attention = fragment.history.attention ?? [];
  const polls: Kept["polls"] = {};
  for (const p of fragment.history.polls ?? []) (polls[p.machine_id] ??= []).push(p.state);
  return { attention: Object.fromEntries(attentionKinds.map(k => [k, attention.map(p => p[k])])) as Kept["attention"], polls };
}
