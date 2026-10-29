import { useCallback, useEffect, useRef, useState } from "react";
import { fleetSpec } from "../fleet/FleetSpec";
import type { Dashboard, Fragment, Prompt } from "../panels/types";
import { chat, dashboards, type Operation, type Summary } from "./api";

export const refreshMs = 5000;
export const headerID = "header";
export const homeID = "home";
const inboxSpec: Dashboard = { name: "Inbox", panels: [{ id: "inbox", title: "Inbox", viz: "inbox", source: "inbox" }] };
type Scope = { id: string; vars: Record<string, string> };
type Freshness = { at: number; failed: boolean };
const unknown: Freshness = { at: 0, failed: false };

/** Owns publication across polls, navigation and edits. Each mutation runs
 * once; refreshes cannot overwrite it or publish into a later scope. Failed
 * supplemental reads retain their last good values independently. */
export function useDashboardRefresh({ id, vars, onError, onMissing }: Scope & { onError: (message: string) => void; onMissing: () => void }) {
  const [header, setHeader] = useState<Fragment | null>(null);
  const [inbox, setInbox] = useState<Fragment | null>(null);
  const [fleet, setFleet] = useState<Fragment | null>(null);
  const [selected, setSelected] = useState<{ id: string; navigation: number; fragment: Fragment | null; fresh: Freshness }>({ id, navigation: 0, fragment: null, fresh: unknown });
  const [prompts, setPrompts] = useState<Prompt[]>([]);
  const [list, setList] = useState<Summary[]>([]);
  const [editing, setEditingState] = useState(false);
  const control = useRef({ scope: { id, vars, mutations: 0, reconcile: false, navigation: 0 }, generation: 0, editing: false, headerMutations: 0 });
  const callbacks = useRef({ onError, onMissing });
  callbacks.current = { onError, onMissing };
  const c = control.current;
  if (c.scope.id !== id || c.scope.vars !== vars) {
    c.scope = { id, vars, mutations: 0, reconcile: false, navigation: c.scope.navigation + Number(c.scope.id !== id) };
    c.generation++;
  }
  const fragment = selected.navigation === c.scope.navigation ? selected.fragment : null;
  const fresh = selected.navigation === c.scope.navigation ? selected.fresh : unknown;

  const refresh = useCallback(async (explicit = false) => {
    const c = control.current;
    if (c.scope.id !== id || c.scope.vars !== vars || c.scope.mutations > 0 || c.headerMutations > 0 || c.editing && !explicit) return;
    const scope = c.scope;
    const generation = ++c.generation;
    const current = () => c.generation === generation && c.scope === scope;
    const [h, i, f] = await Promise.allSettled([dashboards.open(headerID), dashboards.run(inboxSpec), dashboards.run(fleetSpec)]);
    if (!current()) return;
    if (h.status === "fulfilled") setHeader(h.value);
    if (i.status === "fulfilled") setInbox(i.value);
    if (f.status === "fulfilled") setFleet(f.value);
    chat.prompts().then(ps => { if (current()) setPrompts(ps); }, () => undefined);
    try {
      const next = await dashboards.open(id, vars);
      if (current()) setSelected({ id, navigation: scope.navigation, fragment: next, fresh: { at: Date.now(), failed: false } });
    } catch (e) {
      if (!current()) return;
      setSelected(s => ({ id, navigation: scope.navigation, fragment: s.navigation === scope.navigation ? s.fragment : null, fresh: { at: s.navigation === scope.navigation ? s.fresh.at : 0, failed: true } }));
      if (id !== homeID) { callbacks.current.onError((e as Error).message); callbacks.current.onMissing(); }
    }
  }, [id, vars]);

  const latestRefresh = useRef(() => refresh(true));
  latestRefresh.current = () => refresh(true);

  useEffect(() => {
    void refresh(true);
    return () => { control.current.generation++; };
  }, [refresh]);
  useEffect(() => {
    if (editing) return;
    const timer = window.setInterval(() => { if (document.visibilityState === "visible") void refresh(); }, refreshMs);
    return () => window.clearInterval(timer);
  }, [refresh, editing]);
  useEffect(() => {
    let current = true;
    dashboards.list().then(l => { if (current) setList(l.filter(d => d.id !== headerID)); }, () => undefined);
    return () => { current = false; };
  }, [fragment?.id, fragment?.version]);

  const setEditing = useCallback((value: boolean | ((previous: boolean) => boolean)) => {
    const c = control.current;
    const next = typeof value === "function" ? value(c.editing) : value;
    if (next === c.editing) return;
    c.editing = next;
    c.generation++;
    setEditingState(next);
  }, []);

  const edit = async (target: string, base: number, operations: Operation[], message?: string) => {
    const c = control.current;
    const scope = c.scope;
    c.generation++;
    scope.mutations++;
    try {
      const next = await dashboards.edit(target, operations, base, target === headerID ? undefined : scope.vars, message);
      if (c.scope !== scope) return;
      if (target === headerID) setHeader(next);
      else if (target === scope.id) setSelected({ id: scope.id, navigation: scope.navigation, fragment: next, fresh: { at: Date.now(), failed: false } });
      return next;
    } catch (e) {
      if (c.scope === scope) { callbacks.current.onError((e as Error).message); scope.reconcile = true; }
    } finally {
      scope.mutations--;
      if (c.scope === scope) {
        c.generation++;
        if (scope.reconcile && scope.mutations === 0) { scope.reconcile = false; void refresh(true); }
      }
    }
  };

  // Header edits outlive navigation. Polls cannot publish over their response,
  // and a conflict reloads the spec once without replaying the mutation.
  const mutateHeader = async (save: () => Promise<Fragment>) => {
    const c = control.current;
    if (c.headerMutations > 0) return;
    const scope = c.scope;
    c.headerMutations++;
    c.generation++;
    try {
      const next = await save();
      setHeader(next);
      return next;
    } catch (e) {
      const conflict = e instanceof Error && "status" in e && e.status === 409;
      try {
        setHeader(await dashboards.open(headerID));
        callbacks.current.onError(conflict ? "Header changed elsewhere; reloaded" : (e as Error).message);
      } catch (reload) { callbacks.current.onError((reload as Error).message); }
    } finally {
      c.headerMutations--;
      c.generation++;
      if (c.scope !== scope) void latestRefresh.current();
    }
  };

  const forgetPrompt = (id: string) => {
    control.current.generation++;
    setPrompts(ps => ps.filter(p => p.id !== id));
  };
  return { fragment, header, inbox, fleet, prompts, list, fresh, editing, setEditing, refresh: () => refresh(true), edit, mutateHeader, forgetPrompt };
}
