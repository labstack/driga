import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, userEvent, waitFor } from "storybook/test";
import { prompt } from "../../fixtures/chat";
import { App } from "../App";
import { inboxPanel, recorded } from "./requests";

const service = recorded({ prompts: [prompt] });

/** A Claude session asks to run a command; the person reads exactly what
 * it will run and allows it from the inbox, without opening the terminal. */
const meta = {
  id: "flows-answer-a-prompt",
  title: "Flows/Answer a prompt",
  component: App,
  parameters: { layout: "fullscreen", makeService: service.makeService, docs: { story: { inline: false, iframeHeight: 900 } } },
} satisfies Meta<typeof App>;
export default meta;
type Story = StoryObj<typeof meta>;

export const PromptArrives: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByText("Bash(npm run migrate -- --env staging)")).toBeVisible();
    await expect(canvas.getByText("/home/dev/src/atlas")).toBeVisible();
    await expect(canvas.getAllByText("needs approval").length).toBeGreaterThan(0);
  },
};
export const Allowed: Story = {
  play: async ({ canvas, step }) => {
    const allow = (await inboxPanel(canvas)).getByRole("button", { name: /^Approve/ });
    await step("The buttons arm after a moment, so a stray click cannot answer", async () => {
      await waitFor(() => expect(allow).toBeEnabled());
    });
    await userEvent.click(allow);
    await waitFor(() => expect(service.requests).toContainEqual({ path: "/chat/api/prompt-answer", body: { id: "prompt-1", allow: true } }));
    await waitFor(() => expect(canvas.queryByText("Bash(npm run migrate -- --env staging)")).toBeNull());
  },
};
export const Denied: Story = {
  play: async ({ canvas }) => {
    const deny = (await inboxPanel(canvas)).getByRole("button", { name: /^Deny/ });
    await waitFor(() => expect(deny).toBeEnabled());
    await userEvent.click(deny);
    await waitFor(() => expect(service.requests).toContainEqual({ path: "/chat/api/prompt-answer", body: { id: "prompt-1", allow: false } }));
  },
};
