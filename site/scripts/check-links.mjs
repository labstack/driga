// Assert that every internal link in the built site resolves: the page exists
// and, for a link with a fragment, the target page has an element with that id.
//
// The generated reference links its summary tables to headings by slug, and the
// slug is computed in Go (cmd/driga-docgen) while the heading id is computed by
// Starlight. Nothing else notices when the two disagree: a dead in-page anchor
// builds clean and simply scrolls nowhere. Hand-written guides link to each
// other the same way.
import { readdir, readFile } from "node:fs/promises";
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const dist = fileURLToPath(new URL("../dist/", import.meta.url));

async function htmlFiles(dir) {
  const found = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) found.push(...(await htmlFiles(path)));
    else if (entry.name.endsWith(".html")) found.push(path);
  }
  return found;
}

// `/guides/flow` is served from guides/flow.html under build.format "file".
function pageKey(pathname) {
  const clean = pathname.replace(/\/$/, "") || "/index";
  return clean.replace(/^\//, "").replace(/\.html$/, "");
}

let files;
try {
  files = await htmlFiles(dist);
} catch (error) {
  console.error(`check-links: cannot read ${dist} — run \`npm run build\` first`);
  console.error(String(error));
  process.exit(1);
}

const ids = new Map();
const html = new Map();
for (const file of files) {
  const key = relative(dist, file).replace(/\.html$/, "");
  const body = await readFile(file, "utf8");
  html.set(key, body);
  ids.set(key, new Set([...body.matchAll(/\sid="([^"]+)"/g)].map((m) => m[1])));
}

const failures = [];
let checked = 0;
for (const [key, body] of html) {
  if (key === "404") continue;
  for (const match of body.matchAll(/<a\s[^>]*href="([^"]+)"/g)) {
    const href = match[1].replace(/&amp;/g, "&");
    if (/^(https?:|mailto:|\/\/)/.test(href) || href.startsWith("/_astro/")) continue;
    if (/\.(md|txt|xml|svg|png)(#|$)/.test(href)) continue;
    const url = new URL(href, `https://driga.dev/${key}`);
    const target = pageKey(url.pathname);
    checked += 1;
    if (!ids.has(target)) {
      failures.push(`${key}: ${href} (no such page)`);
      continue;
    }
    const fragment = decodeURIComponent(url.hash.slice(1));
    if (fragment && fragment !== "_top" && !ids.get(target).has(fragment)) {
      failures.push(`${key}: ${href} (no #${fragment} on ${target})`);
    }
  }
}

if (failures.length > 0) {
  console.error("check-links: internal links that do not resolve:");
  for (const failure of [...new Set(failures)]) console.error(`  ${failure}`);
  process.exit(1);
}

console.log(`check-links: ${checked} internal link(s) across ${html.size} page(s), all resolve`);
