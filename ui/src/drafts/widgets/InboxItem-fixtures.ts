// Fictional inbox entries, one of each kind, over the projects atlas, harbor,
// beacon and sandbox on the machines studio, build-01 and laptop-02.
import type { InboxEntry } from "./InboxItem";
import { ago } from "./KpiTile-fixtures";

export const permission: InboxEntry = { id: "i1", kind: "permission", title: "Fix the race in the rate limiter", agent: "claude", reason: "Bash(go test -race ./middleware/...)", project: "atlas", branch: "fix/limiter-race", machine: "studio", at: ago(27), since: ago(2.2) };

export const question: InboxEntry = { id: "i2", kind: "question", title: "Add retries to the harbor client", agent: "codex", reason: "Should retries back off exponentially, or keep the fixed 500 ms the other clients use?", project: "harbor", branch: "feat/client-retries", machine: "build-01", at: ago(41), since: ago(6.5) };

export const finished: InboxEntry = { id: "i3", kind: "finished", title: "Migrate the index store to WAL", agent: "gemini", reason: "Edit(internal/index/store.go)", project: "beacon", branch: "main", machine: "studio", at: ago(58), turns: 16, review: { files: 4, uncommitted: 1, unpushed: 2 } };

export const finishedClean: InboxEntry = { id: "i4", kind: "finished", title: "Rename the collector's spans", agent: "copilot", reason: "Edit(internal/collect/span.go)", project: "sandbox", branch: "chore/rename-spans", machine: "laptop-02", at: ago(132), turns: 7, review: { files: 2, uncommitted: 0, unpushed: 0 } };

export const stuck: InboxEntry = { id: "i5", kind: "stuck", title: "Rewrite the dashboard layout tests", agent: "agy", reason: "Bash(bun test src/host)", project: "atlas", branch: "test/layout", machine: "build-01", at: ago(44), since: ago(14), quietExpected: 3 * 60_000 };

export const machine: InboxEntry = { id: "i6", kind: "machine", title: "build-01 is at 94% memory", reason: `${(30.1).toFixed(1)} / 32 GB · swap 6 GB · 3 leftover trees ${(1.8).toFixed(1)} GB`, machine: "build-01", at: ago(9), since: ago(9) };

export const longTitle: InboxEntry = { ...permission, id: "i7", title: "Investigate why the nightly release pipeline leaves the staging bucket half-synchronised after a cancelled run", reason: "Bash(aws s3 sync --delete --dryrun s3://staging-example-com/releases/2026.03 ./out/releases/2026.03 --exclude '*.tmp')" };

export const entries: InboxEntry[] = [permission, question, stuck, machine, finished, finishedClean];
