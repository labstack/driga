// StatusBadge composes Canon's shared status label from Mantine until Driga
// can install @labstack/ui: a tone, an icon and text, so status never relies
// on color alone.
import { Badge } from "@mantine/core";
import { CheckCircle, Circle, HandPalm, Info, Pause, Pulse, WarningOctagon, type Icon } from "@phosphor-icons/react";

export type StatusTone = "neutral" | "info" | "success" | "warning" | "danger";

const look: Record<StatusTone, { color: string; icon: Icon }> = {
  neutral: { color: "gray", icon: Circle },
  info: { color: "info", icon: Info },
  success: { color: "success", icon: CheckCircle },
  warning: { color: "warning", icon: HandPalm },
  danger: { color: "danger", icon: WarningOctagon },
};

// A session state's own icon, so states that share a tone still differ.
const stateIcons: Record<string, Icon> = { working: Pulse, idle: Pause, finished: CheckCircle };

/** toneOf maps a session's state label onto the shared tones: waiting on a
 * person is the one warning and a suspected stall the one danger. Working
 * is information, not success: success is kept for verified outcomes. */
export function toneOf(label: string): StatusTone {
  switch (label.replace(/^~/, "")) {
    case "working": return "info";
    case "waiting": return "warning";
    case "stalled?": return "danger";
    default: return "neutral";
  }
}

/** StatusBadge shows a state; an inferred one, which Driga read from
 * process and file activity rather than the agent, is outlined and says so. */
export function StatusBadge({ tone, children, inferred }: { tone: StatusTone; children: string; inferred?: boolean }) {
  const { color, icon } = look[tone];
  const Glyph = stateIcons[children.replace(/^~/, "")] ?? icon;
  return <Badge color={color} variant={inferred ? "outline" : "light"} leftSection={<Glyph size={12} aria-hidden />} aria-label={inferred ? `${children}, inferred` : children}>{children}</Badge>;
}
