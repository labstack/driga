// StatusBadge composes Canon's shared status label from Mantine until Driga
// can install @labstack/ui: a tone, an icon and text, so status never relies
// on color alone.
import { Badge } from "@mantine/core";
import { CheckCircle, Circle, HandPalm, Info, WarningOctagon, type Icon } from "@phosphor-icons/react";

export type StatusTone = "neutral" | "info" | "success" | "warning" | "danger";

const look: Record<StatusTone, { color: string; icon: Icon }> = {
  neutral: { color: "gray", icon: Circle },
  info: { color: "info", icon: Info },
  success: { color: "success", icon: CheckCircle },
  warning: { color: "warning", icon: HandPalm },
  danger: { color: "danger", icon: WarningOctagon },
};

/** toneOf maps a session's state label onto the shared tones: waiting on a
 * person is the one warning, a suspected stall the one danger. */
export function toneOf(label: string): StatusTone {
  switch (label.replace(/^~/, "")) {
    case "working": return "success";
    case "waiting": return "warning";
    case "stalled?": return "danger";
    case "idle": return "info";
    default: return "neutral";
  }
}

/** StatusBadge shows a state; an inferred one, which Driga read from
 * process and file activity rather than the agent, is outlined and says so. */
export function StatusBadge({ tone, children, inferred }: { tone: StatusTone; children: string; inferred?: boolean }) {
  const { color, icon: Glyph } = look[tone];
  return <Badge color={color} variant={inferred ? "outline" : "light"} leftSection={<Glyph size={12} aria-hidden />} aria-label={inferred ? `${children}, inferred` : children}>{children}</Badge>;
}
