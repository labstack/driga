// Fictional sessions for the session card, one per state.
import { ago, GB, walk } from "./KpiTile-fixtures";
import type { Session } from "./SessionCard";

const MB = 1024 ** 2;

export const working: Session = { id: "s1", title: "Fix the race in the rate limiter", state: "working", agent: "codex", project: "atlas", branch: "fix/limiter-race", machine: "studio", lastTool: "Edit(internal/middleware/limit.go)", lastAt: ago(0.5), turns: 16, files: 4, uncommitted: 1, memoryBytes: 384 * MB, startedAt: ago(122), quietFor: 30_000, quietExpected: 3 * 60_000, trend: walk(21, 30, 4, 2, 0, 9).map(Math.round) };

export const waiting: Session = { id: "s2", title: "Add retries to the harbor client", state: "waiting", agent: "claude", project: "harbor", branch: "feat/client-retries", machine: "build-01", lastTool: "Bash(go test -race ./client/...)", lastAt: ago(2.2), turns: 9, files: 2, uncommitted: 0, memoryBytes: 512 * MB, startedAt: ago(48), quietFor: 2.2 * 60_000, quietExpected: 3 * 60_000, trend: walk(22, 30, 3, 2, 0, 7).map(Math.round) };

export const stuck: Session = { id: "s3", title: "Rewrite the dashboard layout tests", state: "stuck", agent: "agy", project: "atlas", branch: "test/layout", machine: "build-01", lastTool: "Bash(bun test src/host)", lastAt: ago(14), turns: 22, files: 6, uncommitted: 3, memoryBytes: 1.4 * GB, startedAt: ago(190), quietFor: 14 * 60_000, quietExpected: 3 * 60_000, trend: [...walk(23, 16, 5, 2, 1, 9).map(Math.round), ...Array<number>(14).fill(0)] };

export const idle: Session = { id: "s4", title: "Draft the release notes", state: "idle", inferred: true, agent: "gemini", project: "beacon", branch: "main", machine: "laptop-02", lastTool: "Read(CHANGELOG.md)", lastAt: ago(35), turns: 3, files: 0, uncommitted: 0, memoryBytes: 210 * MB, startedAt: ago(70), quietFor: 35 * 60_000, trend: Array<number>(30).fill(0) };

export const finished: Session = { id: "s5", title: "Migrate the index store to WAL", state: "finished", agent: "copilot", project: "beacon", branch: "main", machine: "studio", lastTool: "Edit(internal/index/store.go)", lastAt: ago(58), turns: 16, files: 4, uncommitted: 1, memoryBytes: 0, startedAt: ago(140), trend: [...walk(25, 20, 4, 2, 0, 8).map(Math.round), ...Array<number>(10).fill(0)] };

export const sessions: Session[] = [waiting, stuck, working, finished, idle];
