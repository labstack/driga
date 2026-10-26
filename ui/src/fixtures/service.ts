// A fake Driga service for stories: it answers the app's requests from
// fixtures and records what was asked, and never reaches a real service.
import type { ChatState } from "../host/api";
import type { Dashboard, Fragment, Prompt } from "../panels/types";
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
  const open: Record<string, Fragment> = { header: fragments.header, home: fragments.home, ...o.open };
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
        return f ? json({ ...f, id: String(body?.id), version: f.version ?? 1 }) : new Response("dashboard not found", { status: 404 });
      }
      case "/app/api/run": {
        const spec = body?.dashboard as Dashboard;
        if (spec.name === "Inbox") return json(o.inbox ?? fragments.inbox);
        if (spec.name === "Search") return json(fragments.search);
        if (spec.name === "Fleet") return json(o.fleet ?? fleetFixture);
        return json(fragmentOf(spec));
      }
      case "/app/api/list": return json({ dashboards: Object.keys(open).filter(id => id !== "header").map(id => ({ id, name: open[id].dashboard.name, panels: open[id].dashboard.panels.length, version: 1 })) });
      case "/app/api/edit": case "/app/api/create": return json({ ...open[String(body?.id ?? "home")] ?? fragments.home, version: 2 });
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
      case "/chat/api/send": case "/chat/api/stop": case "/chat/api/decide": case "/chat/api/prompt-answer": return json({ ok: true });
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
