// Renders public/social-card.png, the og:image every page advertises.
//
// The PNG is committed; this script is how it is re-rendered, not part of the
// build. `scripts/check-social-card.mjs` asserts during the build that the
// committed file exists and matches the size declared in Head.astro.
//
// The mark is read from the product's own brand asset
// (internal/web/static/brand/lockup-white.svg), so the card cannot drift from
// the logo the app ships. Colours are Catppuccin Mocha, the app's dark theme.
// Text is rasterised with whatever IBM Plex or sans-serif face the system
// font configuration resolves, which is why the output is committed rather
// than rebuilt in CI.
//
//   node scripts/social-card.mjs
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import sharp from "sharp";

const WIDTH = 1280;
const HEIGHT = 640;

const brand = fileURLToPath(
  new URL("../../internal/web/static/brand/lockup-white.svg", import.meta.url),
);
const out = fileURLToPath(new URL("../public/social-card.png", import.meta.url));

const lockup = await readFile(brand, "utf8");
const inner = lockup
  .replace(/^[\s\S]*?<svg[^>]*>/, "")
  .replace(/<\/svg>\s*$/, "")
  .replace(/<title[^>]*>[\s\S]*?<\/title>/, "");

// The lockup's viewBox is 360x128; scaled to 720 wide it is 256 tall.
const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${WIDTH}" height="${HEIGHT}" viewBox="0 0 ${WIDTH} ${HEIGHT}">
  <rect width="${WIDTH}" height="${HEIGHT}" fill="#1e1e2e"/>
  <rect x="0" y="${HEIGHT - 12}" width="${WIDTH}" height="12" fill="#cba6f7"/>
  <g transform="translate(96 120) scale(2)">${inner}</g>
  <text x="104" y="470" font-family="IBM Plex Sans, Helvetica Neue, Arial, sans-serif" font-size="44" fill="#cdd6f4">See what your coding agents are doing,</text>
  <text x="104" y="528" font-family="IBM Plex Sans, Helvetica Neue, Arial, sans-serif" font-size="44" fill="#cdd6f4">and what needs you.</text>
  <text x="${WIDTH - 104}" y="528" text-anchor="end" font-family="IBM Plex Mono, Menlo, monospace" font-size="30" fill="#cba6f7">driga.dev</text>
</svg>`;

await sharp(Buffer.from(svg)).png({ compressionLevel: 9 }).toFile(out);
console.log(`social-card: wrote ${out}`);
