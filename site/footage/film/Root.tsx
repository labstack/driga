import { loadFont as loadMono } from "@remotion/google-fonts/GeistMono";
import type { ReactNode } from "react";
import { AbsoluteFill, Composition, Img, interpolate, OffthreadVideo, Sequence, staticFile, useCurrentFrame } from "remotion";
import cut from "./cut.json";

// Every moving picture here is a real-time recording of the real driga binary
// on driga-demo's scripted sessions (flow.tape). This file only cuts those
// takes: no zoom animation, no speed change, no captions. cut.json holds the
// moments, and the site reads the same file for the chapters.

const { fontFamily: mono } = loadMono();
const { fps, takes } = cut;
const s = (seconds: number) => Math.round(seconds * fps);
type Theme = "dark" | "light";

// Catppuccin Mocha and Latte, as on driga.dev.
const palette = {
  dark: { page: "#11111b", bar: "#1e1e2e", muted: "#9399b2", text: "#cdd6f4", line: "#313244" },
  light: { page: "#dce0e8", bar: "#e6e9ef", muted: "#5c5f77", text: "#4c4f69", line: "#bcc0cc" },
};

// The terminal take already carries its window (VHS draws it).
function Terminal({ src, from }: { src: string; from: number }) {
  return <OffthreadVideo src={staticFile(src)} startFrom={s(from)} muted style={{ width: "100%", height: "100%" }} />;
}

function EndCard({ theme }: { theme: Theme }) {
  const p = palette[theme];
  const frame = useCurrentFrame();
  const opacity = interpolate(frame, [0, 10], [0, 1], { extrapolateRight: "clamp" });
  return (
    <AbsoluteFill style={{ background: p.page, justifyContent: "center", alignItems: "center", gap: 44, opacity }}>
      <Img src={staticFile(theme === "dark" ? "lockup-white.svg" : "lockup.svg")} style={{ height: 180 }} />
      <div style={{ display: "flex", gap: 18, padding: "20px 32px", borderRadius: 16, background: p.bar, border: `2px solid ${p.line}`, fontFamily: mono, fontSize: 38, color: p.text }}>
        <span style={{ color: p.muted }}>$</span>brew install --cask labstack/tap/driga
      </div>
    </AbsoluteFill>
  );
}

type Shot = [length: number, node: ReactNode];

function Cut({ shots }: { shots: Shot[] }) {
  let at = 0;
  return (
    <AbsoluteFill>
      {shots.map(([length, node], i) => {
        const from = at;
        at += s(length);
        return <Sequence key={i} from={from} durationInFrames={s(length)}>{node}</Sequence>;
      })}
    </AbsoluteFill>
  );
}

const { tui, machines } = takes;

// The terminal from `driga` typed to an answered prompt, then the install
// command.
const films: Record<string, (theme: Theme) => Shot[]> = {
  hero: (theme) => [
    [tui.answered - tui.typed, <Terminal src={`tui-${theme}.mp4`} from={tui.typed} />],
    [cut.hero.end, <EndCard theme={theme} />],
  ],
  machines: (theme) => [[machines.length, <Terminal src={`tui-machines-${theme}.mp4`} from={machines.start} />]],
};

const total = (shots: Shot[]) => shots.reduce((sum, [length]) => sum + s(length), 0);

export function Root() {
  return (
    <>
      {Object.entries(films).flatMap(([id, film]) => (["dark", "light"] as const).map((theme) => {
        const shots = film(theme);
        const [width, height] = id === "hero" ? [1920, 1080] : [1200, 900];
        return <Composition key={`${id}-${theme}`} id={`${id}-${theme}`} component={() => <Cut shots={shots} />} width={width} height={height} fps={fps} durationInFrames={total(shots)} />;
      }))}
    </>
  );
}
