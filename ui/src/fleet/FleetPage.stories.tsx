import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, within } from "storybook/test";
import { fleet, fleetEmpty, fleetQuiet } from "../fixtures/fleet";
import { fragments } from "../fixtures/fragments";
import { FleetPage } from "./FleetPage";

/** The Fleet as the app opens it, from the Fleet spec the service answers:
 * the sessions source's rows with the flow's extras, machines, leftovers,
 * projects and each machine's latest memory sample. */
const meta = {
  id: "pages-fleet",
  title: "Pages/Fleet",
  component: FleetPage,
  args: { fragment: fleet, inbox: fragments.inbox, act: fn(async () => undefined) },
  decorators: [Story => <Box h={900} p="md" style={{ display: "flex", flexDirection: "column" }}><Story /></Box>],
  parameters: {
    layout: "fullscreen",
    docs: { story: { inline: false, iframeHeight: 900 }, description: { component: "Where the memory goes and what needs you, across every machine Driga knows. Built from what the service sends today: states, memory, the flow's parents and last tools, the inbox's reasons for waiting. The service supplies persisted attention and poll history, turn outcomes, captured relative file paths and a 24-hour project tool-call heatmap." } },
  },
} satisfies Meta<typeof FleetPage>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Busy: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("heading", { name: "Fleet", level: 1 })).toBeVisible();
    await expect(canvas.getByText("3 machines · 4 projects · 9 sessions")).toBeVisible();
    await expect(canvas.getByText("· 3 need you")).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^laptop-02 last seen 14m ago/ })).toBeVisible();
    await expect(canvas.getByRole("img", { name: /Fleet flow/ })).toBeVisible();
    await expect(canvas.getByRole("img", { name: /Tool calls by project/ })).toBeVisible();
  },
};

export const OpenASession: Story = {
  name: "Open a session",
  play: async ({ canvas, args }) => {
    await userEvent.click(canvas.getByRole("radio", { name: "Tree" }));
    await userEvent.click(canvas.getByRole("button", { name: "Add rate limiting to the upload API" }));
    const inspector = within(canvas.getByRole("group", { name: "Inspector" }));
    await expect(inspector.getByText("permission · Bash · npm run migrate -- --env staging")).toBeVisible();
    await expect(inspector.getByRole("img", { name: /Last 30 turns/ })).toBeVisible();
    await expect(inspector.getByText("src/upload.ts")).toBeVisible();
    await expect(inspector.getByText("also #15")).toBeVisible();
    await userEvent.click(inspector.getByRole("button", { name: "Open" }));
    await expect(args.act).toHaveBeenCalledWith({ kind: "jump", row: { session_id: 11, machine_id: "studio-id" } });
    await userEvent.unhover(inspector.getByRole("button", { name: "Open" }));
  },
};

export const AllClear: Story = {
  args: { fragment: fleetQuiet, inbox: fragments.inbox_quiet },
  play: async ({ canvas }) => {
    await expect(canvas.queryByText(/need you/)).toBeNull();
    await expect(canvas.getAllByRole("button", { name: /reachable/ })).toHaveLength(3);
  },
};

export const NoMachines: Story = {
  args: { fragment: fleetEmpty },
  play: async ({ canvas }) => { await expect(canvas.getByText("No machines yet")).toBeVisible(); },
};

export const Loading: Story = {
  args: { fragment: null },
  play: async ({ canvas }) => { await expect(canvas.getByText("Loading…")).toBeVisible(); },
};
