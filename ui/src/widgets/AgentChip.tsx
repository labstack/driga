// AgentChip names an agent family: a 20 px circle in the family's
// categorical tint with its glyph, and optionally the family's name. The
// tints (peach, teal, sapphire, sky, pink) are no state color, so a chip is
// never read as a status; a small yellow dot says the agent waits on you. A
// family Driga does not know draws neutral with a generic glyph.
import { Box, Group, Indicator, Text, useComputedColorScheme } from "@mantine/core";
import { Code, Diamond, GithubLogo, Planet, Robot, Sparkle, type Icon } from "@phosphor-icons/react";
import { agentColors } from "./palette";
import { agentLabel, agentOf, type Agent } from "./types";

const glyphs: Record<Agent, Icon> = { claude: Sparkle, codex: Code, gemini: Diamond, copilot: GithubLogo, agy: Planet };

export type AgentChipProps = {
  /** The adapter name the service sends: claude, codex, gemini, copilot, agy. */
  agent: string | undefined;
  /** Show the family's name beside the glyph. */
  name?: boolean;
  /** The agent has a prompt waiting on the person. */
  waiting?: boolean;
  size?: number;
};

export function AgentChip({ agent, name, waiting, size = 20 }: AgentChipProps) {
  const scheme = useComputedColorScheme("dark");
  const c = agentColors(scheme, agent);
  const family = agentOf(agent);
  const Glyph = family ? glyphs[family] : Robot;
  const label = `${agentLabel(agent) || "agent"}${waiting ? ", waiting on you" : ""}`;
  const circle = <Box aria-hidden style={{ width: size, height: size, borderRadius: "50%", background: c.tint, boxShadow: `inset 0 0 0 1px ${c.ring}`, color: c.ink, display: "grid", placeItems: "center", flex: "none" }}>
    <Glyph size={Math.round(size * 0.6)} weight="regular" />
  </Box>;
  const mark = waiting ? <Indicator color="warning" size={7} offset={1} withBorder>{circle}</Indicator> : circle;
  if (!name) return <Box role="img" aria-label={label} title={label} style={{ display: "inline-flex", flex: "none" }}>{mark}</Box>;
  return <Group gap={6} wrap="nowrap" title={label} style={{ display: "inline-flex" }}>
    {mark}
    <Text size="xs" lh={1.2} style={{ whiteSpace: "nowrap" }}>{agentLabel(agent) || "agent"}</Text>
    {waiting && <span className="sr-only">, waiting on you</span>}
  </Group>;
}
