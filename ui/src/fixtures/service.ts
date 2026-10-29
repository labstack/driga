// A fake Driga service for stories: it answers the app's requests from
// fixtures and records what was asked, and never reaches a real service.
import type { Operation, ChatState } from "../host/api";
import type { Dashboard, Fragment, Prompt, Panel } from "../panels/types";
import { fleet as fleetFixture } from "./fleet";
import { fragmentOf, fragments } from "./fragments";

export type Service = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

/** unexpected fails any request no story answers. */
export const unexpected: Service = async input => { throw new Error(`unexpected request in a story: ${String(input instanceof Request ? input.url : input)}`); };

export type ServiceOptions = {
  /** Fragments to open by dashboard id; header and home default to the busy fleet. */
  open?: Record<string, Fragment>;
  /** The inbox the Chat brief reads. */
  inbox?: Fragment;
  /** What the Fleet spec returns; the busy fleet by default. */
  fleet?: Fragment;
  prompts?: Prompt[];
  /** A Chat conversation to show; none starts Chat empty. */
  chat?: ChatState;
  /** down makes every request fail, as when the service is unreachable. */
  down?: boolean;
  /** Called with each request's path and body, for plays to assert on. */
  onRequest?: (path: string, body: unknown) => void;
};

const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } });

export function fakeService(o: ServiceOptions = {}): Service {
  const open: Record<string, Fragment> = structuredClone({ header: fragments.header, home: fragments.home, ...o.open });
  for (const [id, f] of Object.entries(open)) { f.id = id; f.version ??= 0; }
  // Use panel-engine readings for the same source/measure, even after an id
  // changes. Never fabricate a zero for a newly added widget.
  const readings = [fragments.header_catalog, ...Object.values(open), fragments.all];
  const present = (id: string, spec: Dashboard): Fragment => {
    const current = open[id];
    const results = spec.panels.map(p => {
      for (const f of readings) {
        const match = f.dashboard.panels.find(q => q.source === p.source && q.measure === p.measure && q.total === p.total && q.caption === p.caption);
        const result = match && f.results.find(r => r.id === match.id);
        if (result) return { ...structuredClone(result), id: p.id };
      }
      return { id: p.id, status: "empty" as const, message: "No fixture reading" };
    });
    return { ...current, id, version: (current?.version ?? 0) + 1, dashboard: spec, results };
  };
  let waited = false;
  let confirmation = 0;
  const tokens = new Map<string,string>();
  return async (input, init) => {
    const url = new URL(String(input instanceof Request ? input.url : input), "http://story.invalid");
    const body = init?.body ? JSON.parse(String(init.body)) as Record<string, unknown> : undefined;
    o.onRequest?.(url.pathname, body);
    if (o.down) throw new TypeError("Failed to fetch");
    switch (url.pathname) {
      case "/app/api/open": {
        const f = open[String(body?.id)];
        return f ? json({ ...f, id: String(body?.id), version: f.version ?? 0 }) : new Response("dashboard not found", { status: 404 });
      }
      case "/app/api/run": {
        const spec = body?.dashboard as Dashboard;
        if (spec.name === "Inbox") return json(o.inbox ?? fragments.inbox);
        if (spec.name === "Search") return json(fragments.search);
        if (spec.name === "Fleet") return json(o.fleet ?? fleetFixture);
        return json(fragmentOf(spec));
      }
      case "/app/api/list": return json({ dashboards: Object.keys(open).filter(id => id !== "header").map(id => ({ id, name: open[id].dashboard.name, panels: open[id].dashboard.panels.length, version: 1 })) });
      case "/app/api/catalog": return json({ widgets: fragments.header_catalog.dashboard.panels });
      case "/app/api/edit": case "/app/api/replace": case "/app/api/reset": {
        const id = String(body?.id);
        const current = open[id];
        if (!current) return json("dashboard not found", 404);
        if (body?.base_version !== current.version) return json("dashboard changed", 409);
        let spec = structuredClone(current.dashboard);
        if (url.pathname.endsWith("reset")) {
          if (id !== "header") return json("no builtin fixture", 400);
          spec = structuredClone(fragments.header.dashboard);
        } else if (url.pathname.endsWith("replace")) spec = structuredClone(body?.dashboard as Dashboard);
        else for (const op of body?.operations as Operation[]) {
          if (op.op === "remove") spec.panels = spec.panels.filter(p => p.id !== op.id);
          if (op.op === "add" || op.op === "move") {
            const p: Panel | undefined = op.op === "add" ? op.panel : spec.panels.find(p => p.id === op.id);
            if (!p || op.op === "add" && spec.panels.some(q => q.id === p.id)) return json("invalid panel", 400);
            if (op.op === "move") spec.panels = spec.panels.filter(q => q.id !== p.id);
            const before = spec.panels.findIndex(q => q.id === op.before);
            spec.panels.splice(before < 0 ? spec.panels.length : before, 0, p);
          }
          if (op.op === "update") spec.panels = spec.panels.map(p => p.id === op.id ? { ...p, ...op.panel } : p);
          if (op.op === "rename") spec.name = op.name;
        }
        if (id === "header" && (spec.panels.length < 1 || spec.panels.length > 16)) return json("header needs 1 to 16 widgets", 400);
        open[id] = present(id, spec);
        return json(open[id]);
      }
      case "/app/api/create": return json({ ...fragments.home, version: 1 });
      case "/app/api/delete": return json({ ok: true });
      case "/app/action": {
        const {token,...request}=body ?? {};
        if (request.action === "kill" || request.action === "cleanup" || request.action === "ack_inbox" && request.all) {
          const approved=typeof token === "string" ? tokens.get(token) : undefined;
          if (typeof token === "string") tokens.delete(token);
          if (approved !== JSON.stringify(request)) return json({error:"Confirm the action first"},403);
        }
        return json({ok:true});
      }
      case "/app/confirm": {
        if (!body?.request) return json({confirmed:true});
        const token=`story-confirmation-${++confirmation}`;
        tokens.set(token,JSON.stringify(body.request));
        return json({confirmed:true,token});
      }
      case "/chat/api/history": return json(o.chat ? [{ id: o.chat.conversation.id }] : []);
      case "/chat/api/prompts": return json(o.prompts ?? []);
      case "/chat/api/new": return json({ id: "conversation-1" });
      case "/chat/api/send": case "/chat/api/stop": case "/chat/api/mode": case "/chat/api/decide": case "/chat/api/prompt-answer": return json({ ok: true });
      case "/chat/api/wait": {
        // The first wait returns the conversation; later ones wait for a
        // change that never comes, until the page lets go.
        if (!waited && o.chat) { waited = true; return json(o.chat); }
        return new Promise<Response>((_, reject) => init?.signal?.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError"))));
      }
    }
    return unexpected(input, init);
  };
}
