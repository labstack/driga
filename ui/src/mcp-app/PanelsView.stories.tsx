import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn } from "storybook/test";
import { fragmentOf, fragments } from "../fixtures/fragments";
import { PanelsView } from "./PanelsView";

/** The MCP App a chat host such as Claude Desktop shows for every Driga
 * panel tool result. It runs no actions; the chat's own tools do that. */
const meta = {
  id: "embedded-panels-app",
  title: "Embedded/Panels app",
  component: PanelsView,
  args: { fragment: fragmentOf({ name: "Memory right now", panels: [{ id: "stat_total", title: "Memory used", viz: "stat" }, { id: "bar", title: "Memory by session", viz: "bar" }] }), problem: null, onVars: fn() },
  parameters: { layout: "fullscreen" },
  globals: { viewport: { value: "chat", isRotated: false } },
} satisfies Meta<typeof PanelsView>;
export default meta;
type Story = StoryObj<typeof meta>;

export const OnePanel: Story = { args: { fragment: fragmentOf({ name: "Sessions", panels: [{ id: "table", title: "Sessions", viz: "table" }] }) } };
export const Board: Story = { play: async ({ canvas }) => { await expect(canvas.getByRole("heading", { name: "Memory by session" })).toBeVisible(); } };
export const WithVariables: Story = { args: { fragment: { ...fragments.home, options: { project: ["atlas", "beacon", "harbor"], machine: ["build-01", "studio"] } } }, globals: { viewport: { value: "desktop", isRotated: false } } };
export const Failed: Story = { args: { fragment: null, problem: "invalid_spec: panels[0].source: unknown source \"quota\"" }, play: async ({ canvas }) => { await expect(canvas.getByRole("alert")).toBeVisible(); } };
export const Loading: Story = { args: { fragment: null }, play: async ({ canvas }) => { await expect(canvas.getByText("Loading…")).toBeVisible(); } };
export const Disconnected: Story = { args: { fragment: null, disconnected: true }, play: async ({ canvas }) => { await expect(canvas.getByText("This view could not connect.")).toBeVisible(); } };
