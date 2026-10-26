import { ActionIcon, Box, Button, Group, Paper, ScrollArea, Stack, Text, Textarea, Title } from "@mantine/core";
import { Check, Sparkle, X } from "@phosphor-icons/react";
import { useEffect, useRef, useState } from "react";
import { useHost } from "../panels/host";
import { PanelView, type Act } from "../panels/PanelView";
import type { Panel, Result } from "../panels/types";
import { EmptyState } from "../widgets/EmptyState";
import { chat, type ChatAction, type ChatMessage, type ChatState } from "./api";
import { ChatCard } from "./ChatCard";

/** said is whether an action's result says anything; an empty JSON value,
 * as a tool returns when it has nothing to add, does not. */
function said(result?: string): boolean {
  const text = (result ?? "").trim();
  return text !== "" && !["{}", "[]", "null", '""', '"{}"', '"[]"'].includes(text);
}

function ActionCard({ conversation, action }: { conversation: string; action: ChatAction }) {
  const pending = action.status === "pending";
  return <Paper withBorder p="xs" radius="md" style={{ background: "var(--driga-raised)", borderColor: pending ? "var(--mantine-color-warning-filled)" : undefined }}>
    <Text size="sm" fw={600}>{action.title} <Text span size="xs" c="dimmed" fw={400}>· {action.status}</Text></Text>
    {said(action.result) && <Text size="xs" c="dimmed" mt={2}>{action.result}</Text>}
    {pending && <Group gap={6} mt="xs">
      <Button onClick={() => void chat.decide(conversation, action.id, true)}>Allow</Button>
      <Button variant="default" onClick={() => void chat.decide(conversation, action.id, false)}>Deny</Button>
    </Group>}
  </Paper>;
}

/** Brief is what Chat opens with: the inbox and the next steps, posted as if
 * by an assistant that watched while you were away. Nothing runs until you
 * pick something. */
function Brief({ inbox, act, ask }: { inbox?: { panel: Panel; result?: Result; listed: boolean }; act?: Act; ask: (text: string) => void }) {
  const waits = inbox?.result?.frame?.rows.filter(r => r[inbox.result!.frame!.columns.findIndex(c => c.name === "kind")] === "waiting").length ?? 0;
  const { prompts = [] } = useHost();
  const items = (inbox?.result?.frame?.rows.length ?? 0) + prompts.length;
  const suggestions = [
    waits ? "What needs me first, and why?" : "Is anything stuck?",
    "What finished since I was away, and is it safe to merge?",
    "Make a dashboard for the project I am working on",
    "Which processes can I reclaim?",
  ];
  return <Stack gap="sm">
    {waits || prompts.length
      ? <Text size="sm" c="dimmed">{prompts.length > 0 && `${prompts.length} permission ${prompts.length === 1 ? "prompt" : "prompts"} to answer. `}{waits ? `${waits} ${waits === 1 ? "session needs" : "sessions need"} you.` : ""}</Text>
      : <EmptyState kind="all-clear" title="Nothing is waiting on you." />}
    {inbox && !inbox.listed && items > 0 && <PanelView panel={inbox.panel} result={inbox.result} act={act} />}
    <Stack gap={6} align="flex-start">{suggestions.map(s => <Button key={s} variant="default" radius="xl" leftSection={<Sparkle />} onClick={() => ask(s)} styles={{ label: { whiteSpace: "normal", textAlign: "left" } }} h="auto" py={6}>{s}</Button>)}</Stack>
  </Stack>;
}

/** answerLast moves each reply after the tool rows and cards of its turn,
 * up to the next question. */
export function answerLast(messages: ChatMessage[]): ChatMessage[] {
  const out: ChatMessage[] = [];
  let reply: ChatMessage | null = null;
  for (const m of messages) {
    if (m.role === "user") { if (reply) out.push(reply); reply = null; out.push(m); continue; }
    if (m.role === "assistant") { if (reply) out.push(reply); reply = m; continue; }
    out.push(m);
  }
  if (reply) out.push(reply);
  return out;
}

export function Chat({ inbox, act, ask, onOpen, onClose }: {
  inbox?: { panel: Panel; result?: Result; listed: boolean }; act?: Act;
  /** ask puts a question in the compose box for the user to send. */
  ask: { text: string; at: number } | null;
  onOpen: (id: string) => void; onClose?: () => void;
}) {
  const [id, setId] = useState<string | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [actions, setActions] = useState<Map<string, ChatAction>>(new Map());
  const [active, setActive] = useState(false);
  const [draft, setDraft] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  // The last question sent, so a failed reply can be retried even when the
  // capture policy keeps no text.
  const [sent, setSent] = useState("");
  const log = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLTextAreaElement>(null);
  useEffect(() => { if (ask) { setDraft(ask.text); input.current?.focus(); } }, [ask]);

  useEffect(() => {
    chat.history().then(list => { if (list.length) setId(list[0].id); }, () => undefined);
  }, []);
  useEffect(() => {
    if (!id) return;
    const control = new AbortController();
    let cursor = 0;
    const byId = new Map<string, ChatMessage>();
    const flush = () => setMessages([...byId.values()].sort((a, b) => a.created_at.localeCompare(b.created_at)));
    const receive = (s: ChatState) => {
      if (s.reset) { byId.clear(); s.messages.forEach(m => byId.set(m.id, m)); setActions(new Map(s.actions.map(a => [a.id, a]))); cursor = s.cursor; }
      for (const e of s.events ?? []) {
        if (e.seq <= cursor) continue;
        cursor = e.seq;
        if (e.action) setActions(prev => new Map(prev).set(e.action!.id, e.action!));
        if (e.kind === "delta" && e.message_id) { const m = byId.get(e.message_id); if (m) byId.set(m.id, { ...m, text: m.text + (e.text ?? ""), html: "" }); }
        if (e.message) byId.set(e.message.id, e.message);
      }
      if (s.cursor > cursor) cursor = s.cursor;
      setActive(!!s.active);
      flush();
    };
    (async () => {
      while (!control.signal.aborted) {
        try { receive(await chat.wait(id, cursor, control.signal)); setProblem(null); }
        catch { if (control.signal.aborted) return; setProblem("Reconnecting…"); await new Promise(r => setTimeout(r, 1000)); }
      }
    })();
    return () => control.abort();
  }, [id]);
  useEffect(() => { log.current?.scrollTo({ top: log.current.scrollHeight }); }, [messages]);

  const send = async (text: string) => {
    const trimmed = text.trim();
    if (!trimmed) return;
    setDraft("");
    try {
      let conversation = id;
      if (!conversation) { conversation = (await chat.create()).id; setId(conversation); }
      await chat.send(conversation, trimmed, { page: "/app/" });
      setSent(trimmed);
    } catch (e) {
      // A send that never reached the service gives the text back.
      setDraft(d => d || trimmed);
      setProblem((e as Error).message);
    }
  };
  const retry = sent && !active ? <Button size="compact-xs" variant="default" onClick={() => void send(sent)}>Retry</Button> : null;

  // A turn's reply streams into one message made when the turn began, while
  // its tool rows and cards are made as they happen. The reply is shown
  // after them, as the answer that ends the turn.
  const ordered = answerLast(messages);
  const dropped = (m: ChatMessage) => (m.role === "user" || m.role === "assistant") && !m.text.trim() && m.status !== "streaming" && m.status !== "queued";
  const firstDropped = ordered.findIndex(dropped);
  const lastReply = ordered.map(m => m.role).lastIndexOf("assistant");
  // The newest card opens expanded; earlier ones stay a summary.
  const lastCard = ordered.map(m => m.role).lastIndexOf("panel");
  return <Stack component="section" aria-label="Chat" h="100%" gap={0}>
    <Group justify="space-between" px="md" py="sm" style={{ borderBottom: "1px solid var(--driga-border)" }}>
      <Group gap={8} align="baseline"><Title order={2} fz="sm" fw={600}>Chat</Title><Text size="xs" c="dimmed">through your Claude Code login</Text></Group>
      {onClose && <ActionIcon aria-label="Close chat" onClick={onClose}><X /></ActionIcon>}
    </Group>
    <ScrollArea viewportRef={log} style={{ flex: 1 }} type="auto">
      <Stack gap="sm" p="md">
        {messages.every(dropped) && <Brief inbox={inbox} act={act} ask={text => void send(text)} />}
        {ordered.map((m, i) => {
          // Under capture_text = "titles" a turn's text is not kept: earlier
          // turns come back empty. One note stands for all of them; only
          // the latest reply says it failed.
          if ((m.role === "user" || m.role === "assistant") && !m.text.trim()) {
            if (m.status === "streaming" || m.status === "queued") return null;
            if (m.role === "assistant" && m.status === "error" && i === lastReply) return <EmptyState key={m.id} kind="error" title="This reply failed" caption="The error is in the service's log." command="driga service logs" onRetry={sent && !active ? () => void send(sent) : undefined} />;
            if (i !== firstDropped) return null;
            return <Text key={m.id} size="xs" c="dimmed">Earlier messages aren't kept. Set <Text span ff="monospace" fz="inherit">capture_text = "snippets"</Text> to keep Chat history.</Text>;
          }
          if (m.role === "panel") return m.text ? <ChatCard key={m.id} text={m.text} open={i === lastCard} onOpen={onOpen} /> : null;
          if (m.role === "tool") return <Group key={m.id} gap={4} c="dimmed"><Check size={12} /><Text size="xs" ff="monospace">{m.text.replace(/^mcp__driga__/, "")}</Text></Group>;
          if (m.role === "action") { const a = actions.get(m.id); return a && id ? <ActionCard key={m.id} conversation={id} action={a} /> : null; }
          if (m.role === "user") return <Paper key={m.id} px="sm" py={6} radius="lg" maw="85%" style={{ alignSelf: "flex-end", background: "var(--driga-hover)", whiteSpace: "pre-wrap" }}><Text size="sm">{m.text}</Text></Paper>;
          const failed = m.role === "assistant" && m.status === "error" && i === lastReply;
          return <Box key={m.id} fz="sm" className="markdown" c={failed ? "danger" : undefined}>{m.html ? <div dangerouslySetInnerHTML={{ __html: m.html }} /> : <Text size="sm" c="inherit">{m.text}</Text>}{failed && retry && <Box mt={4}>{retry}</Box>}</Box>;
        })}
        {active && <Text size="xs" c="dimmed">Working…</Text>}
        {problem && <Text size="xs" c="danger">{problem}</Text>}
      </Stack>
    </ScrollArea>
    <Box component="form" p="sm" style={{ borderTop: "1px solid var(--driga-border)" }} onSubmit={e => { e.preventDefault(); void send(draft); }}>
      <Textarea ref={input} aria-label="Ask or act" autosize minRows={2} maxRows={8} value={draft} placeholder="Ask about your agents, or tell Driga what to show"
        onChange={e => setDraft(e.currentTarget.value)} onKeyDown={e => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); void send(draft); } }} />
      <Group justify="space-between" mt={8}>
        <Text size="xs" c="dimmed">Actions wait for your approval</Text>
        {active ? <Button variant="default" onClick={() => id && void chat.stop(id)}>Stop</Button> : <Button type="submit" disabled={!draft.trim()}>Send</Button>}
      </Group>
    </Box>
  </Stack>;
}
