import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, screen, spyOn, userEvent, waitFor, within } from "storybook/test";
import { spotlight } from "@mantine/spotlight";
import { fakeService } from "../fixtures/service";
import { Search } from "./Search";

/** ⌘K search over live sessions and those that ended in the last two
 * weeks, and the dashboards. A live session opens its pane; an ended one
 * copies its resume command. */
const meta = {
  id: "components-search",
  title: "Components/Search",
  component: Search,
  args: { list: [{ id: "home", name: "Home", panels: 6, version: 1 }, { id: "memory", name: "Memory", panels: 3, version: 4 }], act: fn(async () => undefined), ask: fn(), open: fn(), say: fn() },
  parameters: { service: fakeService(), docs: { story: { inline: false, iframeHeight: 640 } } },
  beforeEach: () => {
    spyOn(navigator.clipboard, "writeText").mockResolvedValue(undefined);
    // Spotlight keeps whether it is open in a module store; each story starts closed.
    spotlight.close();
    return () => spotlight.close();
  },
} satisfies Meta<typeof Search>;
export default meta;
type Story = StoryObj<typeof meta>;

/** search opens the dialog, waits for it to finish opening, and types. */
async function search(canvas: { getByRole: typeof screen.getByRole }, query = "") {
  await userEvent.click(canvas.getByRole("button", { name: "Search" }));
  const dialog = await screen.findByRole("dialog");
  await waitFor(() => expect(dialog).toBeVisible());
  await waitFor(() => expect(getComputedStyle(dialog).opacity).toBe("1"));
  if (query) await userEvent.type(within(dialog).getByPlaceholderText(/Search sessions by title/), query);
  return dialog;
}

export const Button: Story = { play: async ({ canvas }) => { await expect(canvas.getByRole("button", { name: "Search" })).toBeVisible(); } };
export const Results: Story = {
  play: async ({ canvas }) => {
    const dialog = await search(canvas);
    await expect(await within(dialog).findByRole("button", { name: /Fix the flaky checkout test/ })).toBeVisible();
    await expect(within(dialog).getByRole("button", { name: /Upgrade the image pipeline/ })).toBeInTheDocument();
  },
};
export const OpenALiveSession: Story = {
  play: async ({ canvas, args }) => {
    const dialog = await search(canvas);
    await userEvent.click(await within(dialog).findByRole("button", { name: /Fix the flaky checkout test/ }));
    await expect(args.act).toHaveBeenCalledWith(expect.objectContaining({ kind: "jump" }));
  },
};
export const ResumeAnEndedSession: Story = {
  play: async ({ canvas, args }) => {
    const dialog = await search(canvas);
    await userEvent.click(await within(dialog).findByRole("button", { name: /Split the payments service/ }));
    await expect(navigator.clipboard.writeText).toHaveBeenCalledWith("cd '/home/dev/src/harbor' && codex resume 'd4e5f6'");
    await expect(args.say).toHaveBeenCalled();
  },
};
export const Filtered: Story = {
  play: async ({ canvas }) => {
    const dialog = await search(canvas, "payments");
    // A matched word is highlighted in its own mark, so read the results' text.
    const shown = () => Array.from(dialog.querySelectorAll("[data-action]"), b => b.textContent ?? "");
    await waitFor(() => expect(shown().some(t => t.includes("Split the payments service"))).toBe(true));
    await expect(shown().some(t => t.includes("Write the release notes"))).toBe(false);
  },
};
export const NoMatch: Story = {
  play: async ({ canvas }) => {
    const dialog = await search(canvas, "zebra");
    await expect(await within(dialog).findByText(/No session or dashboard matches/)).toBeVisible();
  },
};
export const ServiceUnreachable: Story = {
  args: { list: [] },
  parameters: { service: fakeService({ down: true }) },
  play: async ({ canvas }) => {
    const dialog = await search(canvas);
    await expect(await within(dialog).findByText(/can't reach its service/)).toBeVisible();
  },
};
