import { afterEach, expect, it, vi } from "vitest";
import { confirm, confirmedAction } from "./api";

afterEach(() => vi.unstubAllGlobals());

it("fails closed when native confirmation is unavailable or malformed", async () => {
  const browser = vi.fn(() => true);
  vi.stubGlobal("window", { confirm: browser });
  for (const response of [
    new Response("", { status: 404 }), new Response("", { status: 500 }),
    new Response("bad"), new Response("null"), new Response("[]"),
    new Response('{}'), new Response('{"confirmed":false}'),
    new Response('{"confirmed":"true"}'), new Response('{"confirmed":1}'),
  ]) {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
    expect(await confirm("Stop?")).toBe(false);
  }
  vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("connection failed")));
  expect(await confirm("Stop?")).toBe(false);
  expect(browser).not.toHaveBeenCalled();
});

it("sends the host token with exactly the approved request", async () => {
  const fetcher = vi.fn()
    .mockResolvedValueOnce(new Response('{"confirmed":true,"token":"one-use"}'))
    .mockResolvedValueOnce(new Response('{"ok":true}'));
  vi.stubGlobal("fetch", fetcher);
  const request = { action: "kill", session_id: 7, machine: "here", force: true };
  expect(await confirmedAction("Stop?", request)).toBe(true);
  expect(fetcher).toHaveBeenCalledTimes(2);
  expect(fetcher.mock.calls[0][0]).toBe("/app/confirm");
  expect(JSON.parse(fetcher.mock.calls[0][1].body)).toEqual({ message: "Stop?", request });
  expect(fetcher.mock.calls[1][0]).toBe("/app/action");
  expect(JSON.parse(fetcher.mock.calls[1][1].body)).toEqual({ ...request, token: "one-use" });
});

it("never calls an action after missing, denied, unavailable, or malformed authorization", async () => {
  const browser = vi.fn(() => true);
  vi.stubGlobal("window", { confirm: browser });
  for (const body of [
    '{"confirmed":true}', '{"confirmed":false,"token":"denied"}',
    '{"confirmed":true,"token":""}', '{"confirmed":true,"token":7}',
    '{"confirmed":true,"token":{}}', '{"confirmed":true,"token":[]}',
    '{"confirmed":"true","token":"forged"}', 'null', 'bad',
  ]) {
    const fetcher = vi.fn().mockResolvedValueOnce(new Response(body)).mockResolvedValue(new Response('{"ok":true}'));
    vi.stubGlobal("fetch", fetcher);
    expect(await confirmedAction("Stop?", { action: "kill" })).toBe(false);
    expect(fetcher).toHaveBeenCalledTimes(1);
  }
  for (const status of [404, 503]) {
    const fetcher = vi.fn().mockResolvedValue(new Response("unavailable", { status }));
    vi.stubGlobal("fetch", fetcher);
    expect(await confirmedAction("Stop?", { action: "kill" })).toBe(false);
    expect(fetcher).toHaveBeenCalledTimes(1);
  }
  const fetcher = vi.fn().mockRejectedValue(new TypeError("connection failed"));
  vi.stubGlobal("fetch", fetcher);
  expect(await confirmedAction("Stop?", { action: "kill" })).toBe(false);
  expect(fetcher).toHaveBeenCalledTimes(1);
  expect(browser).not.toHaveBeenCalled();
});

it("does not replay an action when the approved request fails", async () => {
  const fetcher = vi.fn()
    .mockResolvedValueOnce(new Response('{"confirmed":true,"token":"one-use"}'))
    .mockRejectedValueOnce(new TypeError("action response lost"));
  vi.stubGlobal("fetch", fetcher);
  await expect(confirmedAction("Stop?", { action: "kill" })).rejects.toThrow("action response lost");
  expect(fetcher).toHaveBeenCalledTimes(2);
});
