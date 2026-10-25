// Renders every film in both themes from the takes in out/ into out/media
// (MP4 H.264 and WebM VP9), and its poster into src/assets/media, where Astro
// optimises it. upload.mjs publishes out/media to R2.
import { execFileSync } from "node:child_process";
import { copyFileSync, mkdirSync, readFileSync, readdirSync } from "node:fs";

const cut = JSON.parse(readFileSync("film/cut.json", "utf8"));
const media = "out/media";
const posters = "../src/assets/media";
mkdirSync(media, { recursive: true });
mkdirSync(posters, { recursive: true });
copyFileSync("../src/assets/lockup-white.svg", "out/lockup-white.svg");
copyFileSync("../src/assets/lockup.svg", "out/lockup.svg");
const remotion = (...args) => execFileSync("npx", ["remotion", ...args, "--public-dir", "out", "--log", "error"], { stdio: "inherit" });

// The hero's poster is the permission prompt, the film's main point.
const posterFrame = { hero: cut.hero.poster * cut.fps, machines: 1.5 * cut.fps };
for (const id of ["hero", "machines"]) {
  for (const theme of ["dark", "light"]) {
    const name = `${id}-${theme}`;
    remotion("render", "film/index.ts", name, `${media}/${name}.mp4`, "--codec", "h264", "--crf", "23", "--pixel-format", "yuv420p");
    remotion("render", "film/index.ts", name, `${media}/${name}.webm`, "--codec", "vp9", "--crf", "36");
    remotion("still", "film/index.ts", name, `${posters}/${name}.jpg`, "--frame", String(Math.round(posterFrame[id])), "--image-format", "jpeg", "--jpeg-quality", "85");
  }
}

// astro dev plays these until the films are published (see Film.astro).
mkdirSync("../public/media-local", { recursive: true });
for (const file of readdirSync(media)) copyFileSync(`${media}/${file}`, `../public/media-local/${file}`);
