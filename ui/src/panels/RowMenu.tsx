// A row's actions, out of the way until wanted: clicking the row runs its
// primary action, opening the session's pane, and one ⋯ button, shown on
// hover, keyboard focus or touch, lists the rest. Destructive actions are
// red and still ask the app's confirmation first.
import { ActionIcon, Menu } from "@mantine/core";
import { ArrowSquareOut, Check, DotsThree, Lightning, Sparkle, StopCircle, Trash, XCircle, type Icon } from "@phosphor-icons/react";
import type { KeyboardEvent, MouseEvent } from "react";
import { actionLabel, type Act, type ActionKind } from "./host";
import type { Row } from "./types";

const icons: Record<ActionKind, Icon> = { jump: ArrowSquareOut, open: ArrowSquareOut, ack: Check, stop: StopCircle, kill: XCircle, force_kill: Lightning, cleanup: Trash, summarize: Sparkle };
const destructive = new Set<ActionKind>(["stop", "kill", "force_kill", "cleanup"]);

/** primary is the action a click on the row runs, when it has one. */
export function primary(kinds: ActionKind[]): ActionKind | undefined {
  return kinds.find(k => k === "jump" || k === "open");
}

/** rowProps make a row run its primary action on click or Enter. */
export function rowProps(kinds: ActionKind[], row: Row, act?: Act) {
  const kind = primary(kinds);
  if (!act || !kind) return {};
  const run = () => void act({ kind, row });
  return {
    tabIndex: 0, title: actionLabel(kind), className: "row-act",
    onClick: (e: MouseEvent) => { if (!(e.target as HTMLElement).closest("button, a, [role=menu]")) run(); },
    onKeyDown: (e: KeyboardEvent) => { if (e.key === "Enter" && e.target === e.currentTarget) { e.preventDefault(); run(); } },
  };
}

export function RowMenu({ kinds, row, act, label }: { kinds: ActionKind[]; row: Row; act: Act; label: string }) {
  if (kinds.length === 0) return null;
  const first = primary(kinds);
  const rest = kinds.filter(k => k !== first);
  const item = (kind: ActionKind) => {
    const Glyph = icons[kind];
    return <Menu.Item key={kind} color={destructive.has(kind) ? "danger" : undefined} leftSection={<Glyph />} onClick={() => void act({ kind, row })}>{actionLabel(kind)}</Menu.Item>;
  };
  return <Menu position="bottom-end" withinPortal shadow="md" width={180}>
    <Menu.Target>
      <ActionIcon className="row-menu" aria-label={`Actions for ${label}`} onClick={e => e.stopPropagation()}><DotsThree size={18} weight="bold" /></ActionIcon>
    </Menu.Target>
    <Menu.Dropdown>
      {first && item(first)}
      {first && rest.length > 0 && <Menu.Divider />}
      {rest.map(item)}
    </Menu.Dropdown>
  </Menu>;
}
