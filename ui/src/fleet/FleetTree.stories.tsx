import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, within } from "storybook/test";
import { busy } from "./Fleet-fixtures";
import { FleetTree } from "./FleetTree";

/** The Fleet as an accessible table: machines expand into projects into
 * sessions, with subagents one level deeper. */
const meta = {
  id: "fleet-tree",
  title: "Components/Fleet/Tree",
  component: FleetTree,
  args: { data: busy, selected: null, onSelect: fn(), onOpen: fn(), onReply: fn() },
  parameters: { docs: { description: { component: "Tree mode of the Fleet view, and the mode for the keyboard and screen readers: every row title is a button that selects, every machine row expands and collapses, and Reply and Open appear on hover or focus. States are the dot and word, as every table shows them." } } },
} satisfies Meta<typeof FleetTree>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Busy: Story = {
  play: async ({ canvas, args }) => {
    await expect(canvas.getByRole("table", { name: "Fleet tree" })).toBeVisible();
    await expect(canvas.getAllByRole("button", { name: /^(Collapse|Expand) / })).toHaveLength(3);
    await expect(canvas.getAllByText("↳")).toHaveLength(2);
    await expect(canvas.getAllByLabelText("stuck, inferred")).toHaveLength(2);
    await userEvent.click(canvas.getByRole("button", { name: "Collapse laptop-02" }));
    await expect(canvas.queryByRole("button", { name: "Harbor TLS rotation" })).toBeNull();
    await userEvent.click(canvas.getByRole("button", { name: "Expand laptop-02" }));
    await expect(canvas.getByRole("button", { name: "Harbor TLS rotation" })).toBeVisible();
    await userEvent.click(canvas.getByRole("button", { name: "Write the release notes" }));
    await expect(args.onSelect).toHaveBeenCalledWith({ kind: "session", id: "113" });
    await userEvent.unhover(canvas.getByRole("button", { name: "Write the release notes" }));
  },
};

export const Selected: Story = {
  args: { selected: { kind: "session", id: "113" } },
  play: async ({ canvas, args }) => {
    const title = canvas.getByRole("button", { name: "Write the release notes" });
    await expect(title).toHaveAttribute("aria-pressed", "true");
    await userEvent.click(within(title.closest("tr")!).getByRole("button", { name: "Reply" }));
    await expect(args.onReply).toHaveBeenCalledWith(expect.objectContaining({ id: "113" }));
    await userEvent.unhover(title);
  },
};
