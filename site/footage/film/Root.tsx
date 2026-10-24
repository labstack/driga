import { loadFont as loadGeist } from "@remotion/google-fonts/Geist";
import { loadFont as loadMono } from "@remotion/google-fonts/GeistMono";
import type { ReactNode } from "react";
import { AbsoluteFill, Composition, Img, interpolate, OffthreadVideo, Sequence, staticFile, useCurrentFrame } from "remotion";
import cut from "./cut.json";

// Every moving picture here is a real-time recording of the real driga binary
// on driga-demo's scripted sessions (flow.tape, web.mjs). This file only cuts
// those takes, crops the feature loops, and puts the browser take in a browser
// window: no zoom animation, no speed change, no captions. cut.json holds the
// moments and crops, and render.mjs reads the same file for the chapters.

const { fontFamily: sans } = loadGeist();
const { fontFamily: mono } = loadMono();
const { fps, takes, crops } = cut;
const s = (seconds: number) => Math.round(seconds * fps);
type Theme = "dark" | "light";

// Catppuccin Mocha and Latte, as on driga.dev.
const palette = {
  dark: { page: "#11111b", bar: "#1e1e2e", field: "#181825", muted: "#9399b2", text: "#cdd6f4", line: "#313244" },
  light: { page: "#dce0e8", bar: "#e6e9ef", field: "#ccd0da", muted: "#5c5f77", text: "#4c4f69", line: "#bcc0cc" },
};

// The terminal take already carries its window (VHS draws it).
function Terminal({ src, from }: { src: string; from: number }) {
  return <OffthreadVideo src={staticFile(src)} startFrom={s(from)} muted style={{ width: "100%", height: "100%" }} />;
}

// The browser take, in a window matching the terminal's margin and corners.
function Browser({ theme, from }: { theme: Theme; from: number }) {
  const p = palette[theme];
  return (
    <AbsoluteFill style={{ background: p.page, padding: 44 }}>
      <div style={{ flex: 1, display: "flex", flexDirection: "column", borderRadius: 14, overflow: "hidden" }}>
        <div style={{ height: 44, flex: "0 0 auto", display: "flex", alignItems: "center", gap: 9, padding: "0 18px", background: p.bar }}>
          {["#ff5f57", "#febc2e", "#28c840"].map((dot) => <span key={dot} style={{ width: 13, height: 13, borderRadius: 7, background: dot }} />)}
          <div style={{ margin: "0 auto", width: 560, height: 28, borderRadius: 8, display: "flex", alignItems: "center", justifyContent: "center", background: p.field, fontFamily: sans, fontSize: 15, color: p.muted }}>
            127.0.0.1 · driga web
          </div>
          <span style={{ width: 57 }} />
        </div>
        <OffthreadVideo src={staticFile(`web-hero-${theme}.webm`)} startFrom={s(from)} muted style={{ width: "100%", flex: 1, objectFit: "cover", objectPosition: "top" }} />
      </div>
    </AbsoluteFill>
  );
}

// One panel of the detail take, scaled to fill the loop's frame.
function Crop({ theme, from, crop }: { theme: Theme; from: number; crop: { x: number; y: number; width: number; height: number } }) {
  const scale = 1200 / crop.width;
  return (
    <AbsoluteFill style={{ overflow: "hidden", background: palette[theme].page }}>
      <OffthreadVideo src={staticFile(`web-detail-${theme}.webm`)} startFrom={s(from)} muted
        style={{ position: "absolute", width: 1920 * scale, height: 1080 * scale, left: -crop.x * scale, top: -crop.y * scale, maxWidth: "none" }} />
    </AbsoluteFill>
  );
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

const { tui, web, machines } = takes;

// The terminal from `driga` typed to an answered prompt, the same prompt in
// the browser, then the install command.
const films: Record<string, (theme: Theme) => Shot[]> = {
  hero: (theme) => [
    [tui.answered - tui.typed, <Terminal src={`tui-${theme}.mp4`} from={tui.typed} />],
    [web.selected - web.prompt, <Browser theme={theme} from={web.prompt} />],
    [cut.hero.end, <EndCard theme={theme} />],
  ],
  needs: (theme) => [[web.allowed - web.prompt + 1.5, <Crop theme={theme} from={web.prompt} crop={crops.needs} />]],
  flow: (theme) => [[web.selected - web.overlap + 2, <Crop theme={theme} from={web.overlap} crop={crops.flow} />]],
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
