import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, userEvent, waitFor } from "storybook/test";
import { App } from "../App";
import { inboxPanel, recorded } from "./requests";

const service = recorded();

/** A session finished: the inbox says what it left in its checkout, and
 * the person marks it reviewed once they have looked. */
const meta = {
  id: "flows-review-finished-work",
  title: "Flows/Review finished work",
  component: App,
  parameters: { layout: "fullscreen", makeService: service.makeService, docs: { story: { inline: false, iframeHeight: 900 } } },
} satisfies Meta<typeof App>;
export default meta;
type Story = StoryObj<typeof meta>;

export const ToReview: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/To review · 3/)).toBeVisible();
    await expect(canvas.getByText("3 uncommitted")).toBeVisible();
    await expect(canvas.getByText("clean")).toBeVisible();
  },
};
export const MarkedReviewed: Story = {
  play: async ({ canvas }) => {
    const inbox = await inboxPanel(canvas);
    await userEvent.click(inbox.getByRole("button", { name: "Mark Fix the flaky checkout test reviewed" }));
    // One row stands for both of the session's turns; both are acknowledged.
    await waitFor(() => expect(service.requests.filter(r => r.path === "/app/action").map(r => (r.body as { id: string }).id)).toEqual(["done-12a", "done-12b"]));
  },
};
export const OpenThePane: Story = {
  play: async ({ canvas }) => {
    const inbox = await inboxPanel(canvas);
    await userEvent.click(inbox.getByTitle("Write the release notes"));
    await waitFor(() => expect(service.requests).toContainEqual({ path: "/app/action", body: { action: "jump", session_id: 15, machine: "studio-id" } }));
  },
};
