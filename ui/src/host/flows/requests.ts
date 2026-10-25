// What a flow's fake service was asked, for plays to assert on.
import { fakeService, type ServiceOptions } from "../../fixtures/service";

import { expect, within } from "storybook/test";

/** inboxPanel waits for Home to load and returns its Inbox panel; until then
 * Chat's brief shows a copy of the inbox that goes away. */
export async function inboxPanel(canvas: ReturnType<typeof within>) {
  await expect(await canvas.findByRole("heading", { name: "Home", level: 1 })).toBeVisible();
  return within(await canvas.findByRole("article", { name: "Inbox" }));
}

export type Request = { path: string; body: unknown };

/** recorded makes a fresh fake service per run and the list it records into. */
export function recorded(options: ServiceOptions = {}) {
  const requests: Request[] = [];
  return { requests, makeService: () => { requests.length = 0; return fakeService({ ...options, onRequest: (path, body) => requests.push({ path, body }) }); } };
}
