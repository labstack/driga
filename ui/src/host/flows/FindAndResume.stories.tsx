import { spotlight } from "@mantine/spotlight";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, screen, spyOn, userEvent, waitFor, within } from "storybook/test";
import { App } from "../App";
import { recorded } from "./requests";

const service = recorded();

/** The person finds yesterday's work with ⌘K: a live session opens its
 * pane, an ended one copies the command that resumes it. */
const meta = {
  id: "flows-find-and-resume",
  title: "Flows/Find and resume",
  component: App,
  parameters: {
    layout: "fullscreen", makeService: service.makeService, docs: { story: { inline: false, iframeHeight: 900 } },
    // Mantine draws the dialog's title in a <header> inside the modal; axe reads
    // it as a second banner, though the page behind the modal is inert.
    a11y: { config: { rules: [{ id: "landmark-no-duplicate-banner", enabled: false }, { id: "landmark-unique", enabled: false }] } },
  },
  beforeEach: () => {
    spyOn(navigator.clipboard, "writeText").mockResolvedValue(undefined);
    spotlight.close();
    return () => spotlight.close();
  },
} satisfies Meta<typeof App>;
export default meta;
type Story = StoryObj<typeof meta>;

async function open(canvas: { findByRole: typeof screen.findByRole }) {
  await userEvent.click(await canvas.findByRole("button", { name: "Search" }));
  const dialog = await screen.findByRole("dialog", { name: "Search sessions and dashboards" });
  await waitFor(() => expect(getComputedStyle(dialog).opacity).toBe("1"));
  return dialog;
}

export const Opened: Story = {
  play: async ({ canvas }) => {
    const dialog = await open(canvas);
    await expect(await within(dialog).findByRole("button", { name: /Split the payments service/ })).toBeInTheDocument();
  },
};
export const ResumeCopied: Story = {
  play: async ({ canvas }) => {
    const dialog = await open(canvas);
    await userEvent.click(await within(dialog).findByRole("button", { name: /Split the payments service/ }));
    await expect(navigator.clipboard.writeText).toHaveBeenCalledWith("cd '/home/dev/src/harbor' && codex resume 'd4e5f6'");
    const note = await screen.findByText(/Copied the command that resumes Split the payments service/);
    await waitFor(() => expect(note).toBeVisible());
  },
};
export const LivePaneOpened: Story = {
  play: async ({ canvas }) => {
    const dialog = await open(canvas);
    await userEvent.click(await within(dialog).findByRole("button", { name: /Fix the flaky checkout test/ }));
    await waitFor(() => expect(service.requests).toContainEqual({ path: "/app/action", body: { action: "jump", session_id: 12, machine: "studio-id" } }));
  },
};
