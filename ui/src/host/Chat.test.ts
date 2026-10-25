import { expect, it } from "vitest";
import type { ChatMessage } from "./api";
import { answerLast } from "./Chat";

const m = (id: string, role: ChatMessage["role"]) => ({ id, role, text: id, status: "done", created_at: id }) as ChatMessage;

it("shows each reply after its turn's tools and cards", () => {
  const order = answerLast([m("q1", "user"), m("a1", "assistant"), m("t1", "tool"), m("p1", "panel"), m("q2", "user"), m("a2", "assistant"), m("t2", "tool")]).map(x => x.id);
  expect(order).toEqual(["q1", "t1", "p1", "a1", "q2", "t2", "a2"]);
});
