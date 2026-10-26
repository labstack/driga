// EmptyState is a panel with nothing to show, kept to one 56 px row rather
// than a void: a tinted circle with an icon, the message, and a caption that
// explains the emptiness with a count and a time. All-clear is green, an
// error is red with a retry, and a plain empty is neutral.
import { Button, Group, Stack, Text, ThemeIcon } from "@mantine/core";
import { ArrowClockwise, CheckCircle, Tray, WarningCircle } from "@phosphor-icons/react";
import { exact } from "../panels/format";
import "./widgets.css";

export type EmptyStateProps = {
  kind?: "empty" | "all-clear" | "error";
  title: string;
  /** What will appear and how, or what explains the emptiness. */
  caption?: string;
  /** When the check ran, shown as a time with the exact UTC value on hover. */
  checkedAt?: string;
  /** An error's retry. */
  onRetry?: () => void;
  /** An error's command to run by hand, in the inset mono box. */
  command?: string;
  /** One action at most, such as "Learn how". */
  action?: { label: string; onClick: () => void };
};

const look = { empty: { color: "gray", icon: Tray }, "all-clear": { color: "success", icon: CheckCircle }, error: { color: "danger", icon: WarningCircle } } as const;

function clock(iso: string): string {
  return new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

/** splitMessage reads a panel's empty message as a title and a caption:
 * the first sentence, then the rest. */
export function splitMessage(message: string): { title: string; caption?: string } {
  const m = /^(.+?[.!?])\s+(.+)$/s.exec(message.trim());
  return m ? { title: m[1], caption: m[2] } : { title: message.trim() };
}

export function EmptyState({ kind = "empty", title, caption, checkedAt, onRetry, command, action }: EmptyStateProps) {
  const { color, icon: Glyph } = look[kind];
  return <Group mih={56} px="sm" py={6} gap="sm" wrap="nowrap" role={kind === "error" ? "alert" : "status"}>
    <ThemeIcon variant="light" color={color} radius="xl" size={28} aria-hidden styles={{ root: { background: `var(--driga-tint-${kind === "empty" ? "neutral" : color})` } }}><Glyph size={16} /></ThemeIcon>
    <Stack gap={2} style={{ flex: 1, minWidth: 0 }}>
      <Text size="md" fw={500} lh={1.2}>{title}</Text>
      {(caption || checkedAt || command) && <Group gap={6} wrap="wrap">
        {caption && <Text size="xs" c="dimmed" lh={1.3}>{caption}</Text>}
        {checkedAt && <Text size="xs" c="dimmed" ff="monospace" lh={1.3} title={exact(checkedAt)}>{caption ? "· " : ""}checked {clock(checkedAt)}</Text>}
        {command && <Text size="xs" ff="monospace" className="w-inset" lh={1.3} style={{ display: "inline-block" }}>{command}</Text>}
      </Group>}
    </Stack>
    {onRetry && <Button size="compact-xs" variant="subtle" color="gray" leftSection={<ArrowClockwise size={12} />} onClick={onRetry} style={{ flex: "none" }}>Retry</Button>}
    {action && <Button size="compact-xs" variant="subtle" onClick={action.onClick} style={{ flex: "none" }}>{action.label}</Button>}
  </Group>;
}
