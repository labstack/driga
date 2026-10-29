import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, within } from "storybook/test";
import { busy, oneMachine, waitingSession } from "./Fleet-fixtures";
import { FleetTree } from "./FleetTree";

/** The Fleet as an accessible table: machines expand into projects into
 * sessions, with subagents nested under their parents. */
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
    await expect(args.onSelect).toHaveBeenCalledWith({ kind: "session", id: "113", machine: "studio" });
    await userEvent.unhover(canvas.getByRole("button", { name: "Write the release notes" }));
  },
};

export const Selected: Story = {
  args: { selected: { kind: "session", id: "113", machine: "studio" } },
  play: async ({ canvas, args }) => {
    const title = canvas.getByRole("button", { name: "Write the release notes" });
    await expect(title).toHaveAttribute("aria-pressed", "true");
    await userEvent.click(within(title.closest("tr")!).getByRole("button", { name: "Reply to Write the release notes" }));
    await expect(args.onReply).toHaveBeenCalledWith(expect.objectContaining({ id: "113", machine: "studio" }));
    await userEvent.click(within(title.closest("tr")!).getByRole("button", { name: "Open Write the release notes" }));
    await expect(args.onOpen).toHaveBeenCalledWith(waitingSession);
    await userEvent.unhover(title);
  },
};
import { Box } from "@mantine/core";

export const ConstrainedNames: Story = {
  args: { data: {
    ...oneMachine,
    projects: [{ id: "spare", name: "spare with a very long project name that should truncate before the count", repo: "/spare" }],
    sessions: ["1037", "1036", "1028"].map((id, i) => ({ ...oneMachine.sessions[0], id, project: "spare", title: `Long nested session name ${id} that needs a proper ellipsis`, parent: i ? ["1037", "1036"][i - 1] : undefined })),
  } },
  render: args => <Box w={1000} maw="100%"><FleetTree {...args} /></Box>,
  play: async ({ canvas }) => {
    await document.fonts.ready;
    const table = canvas.getByRole("table", { name: "Fleet tree" });
    const name = within(table).getByRole("columnheader", { name: "Name" });
    const branch = within(table).getByRole("columnheader", { name: "Branch" });
    expect(name.getBoundingClientRect().width).toBeGreaterThanOrEqual(340);
    expect(branch.getBoundingClientRect().width).toBeLessThanOrEqual(111);
    const project = canvas.getByRole("button", { name: /^spare with/ });
    const count = within(project.closest("tr")!).getByText("3 sessions");
    expect(getComputedStyle(count).whiteSpace).toBe("nowrap");
    expect(count.getBoundingClientRect().height).toBeLessThan(24);
    const projectText = project.querySelector("p")!;
    expect(getComputedStyle(projectText).textOverflow).toBe("ellipsis");
    expect(projectText.scrollWidth).toBeGreaterThan(projectText.clientWidth);
    expect(Math.abs(project.getBoundingClientRect().top - count.getBoundingClientRect().top)).toBeLessThan(4);
    for (const id of ["1037", "1036", "1028"]) {
      const identifier = canvas.getByText(`#${id}`);
      const cell = identifier.closest("td")!.getBoundingClientRect();
      expect(identifier.getBoundingClientRect().right).toBeLessThanOrEqual(cell.right);
      expect(getComputedStyle(identifier).flexShrink).toBe("0");
      const title = canvas.getByTitle(`Long nested session name ${id} that needs a proper ellipsis`);
      expect(getComputedStyle(title).textOverflow).toBe("ellipsis");
      expect(title.getBoundingClientRect().width).toBeGreaterThan(140);
    }
  },
};

export const NarrowNames: Story = {
  ...ConstrainedNames,
  render: args => <Box w={640} maw="100%"><FleetTree {...args} /></Box>,
};
