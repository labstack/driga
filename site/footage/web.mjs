// One continuous, real-time take of the real `driga web` dashboard, served
// from a driga-demo data directory, in dark and light: a visible pointer
// moves to Allow, answers the permission prompt, then opens the session that
// shares a file with another. Run through `just site-footage`.
import { execFileSync, spawn } from "node:child_process";
import { mkdir, rename, rm } from "node:fs/promises";
import { join } from "node:path";
import { chromium } from "playwright";

const { FOOTAGE_DEMO: demoBin, FOOTAGE_DRIGA: driga, FOOTAGE_DIR: root } = process.env;
if (!demoBin || !driga || !root) throw new Error("FOOTAGE_DEMO, FOOTAGE_DRIGA and FOOTAGE_DIR are required");
// Two takes per theme. "hero" is the whole dashboard at the film's browser
// window content size, laid out 1280 CSS px wide and painted at 1920. "detail"
// is laid out 1067 px wide at 1.8x, so the page's feature loops can crop one
// panel and still show readable text.
const takes = { hero: { size: { width: 1920, height: 994 }, zoom: 1.5 }, detail: { size: { width: 1920, height: 1080 }, zoom: 1.8 } };
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// The viewer reads the demo only while it looks like a running collector;
// otherwise it would snapshot this machine. Never film until it does.
async function demoReady(env) {
  for (let i = 0; i < 100; i++) {
    try {
      const out = execFileSync(driga, ["summary", "--json"], { env, stdio: ["ignore", "pipe", "ignore"] });
      if (JSON.parse(String(out)).mode === "daemon") return;
    } catch {}
    await sleep(100);
  }
  throw new Error("driga-demo did not come up; refusing to record a snapshot of this machine");
}

// A drawn pointer: headless Chromium records no cursor of its own.
function pointer() {
  const el = document.createElement("div");
  el.innerHTML = '<svg width="28" height="28" viewBox="0 0 24 24"><path d="M4 2l15 11-6.5 1.2L16 21l-3 1.4-3.4-6.8L4 20z" fill="#fff" stroke="#11111b" stroke-width="1.4" stroke-linejoin="round"/></svg>';
  Object.assign(el.style, { position: "fixed", left: "0", top: "0", zIndex: "2147483647", pointerEvents: "none", transform: "translate(640px, 520px)", transition: "scale .08s" });
  addEventListener("mousemove", (e) => { el.style.transform = `translate(${e.clientX - 3}px, ${e.clientY - 2}px)`; }, true);
  addEventListener("mousedown", () => { el.style.scale = "0.86"; }, true);
  addEventListener("mouseup", () => { el.style.scale = "1"; }, true);
  // Outside the zoomed body, so it moves in viewport pixels like the events.
  document.addEventListener("DOMContentLoaded", () => document.documentElement.append(el));
}

// Glide like a hand: eased, about 60 steps a second.
let at = { x: 640, y: 520 };
async function glide(page, locator, ms = 700) {
  const box = await locator.boundingBox();
  const to = { x: box.x + box.width / 2, y: box.y + box.height / 2 };
  const steps = Math.round(ms / 16);
  for (let i = 1; i <= steps; i++) {
    const t = i / steps;
    const e = t < 0.5 ? 2 * t * t : 1 - (-2 * t + 2) ** 2 / 2;
    await page.mouse.move(at.x + (to.x - at.x) * e, at.y + (to.y - at.y) * e);
    await sleep(16);
  }
  at = to;
}

async function take(name, scheme) {
  const { size, zoom } = takes[name];
  const dir = join(root, `web-${name}-${scheme}`);
  await rm(dir, { recursive: true, force: true });
  const data = join(dir, "data");
  const env = { ...process.env, HOME: join(dir, "home"), DRIGA_DATA_DIR: data, DRIGA_SPOOL_DIR: join(data, "events"), DRIGA_CONFIG: join(data, "config.toml") };
  await mkdir(env.HOME, { recursive: true });
  const children = [];
  let browser, context, page;
  let ok = false;
  try {
    children.push(spawn(demoBin, ["--data-dir", data], { env, stdio: ["ignore", "ignore", "inherit"] }));
    await demoReady(env);
    const web = spawn(driga, ["web", "--listen", "127.0.0.1:0"], { env, stdio: ["ignore", "pipe", "pipe"] });
    children.push(web);
    const url = await new Promise((resolve, reject) => {
      web.stderr.on("data", (b) => { if (/snapshot/i.test(String(b))) reject(new Error("driga web fell back to a snapshot of this machine")); });
      web.stdout.on("data", (b) => { const m = String(b).match(/http:\/\/\S+/); if (m) resolve(m[0]); });
      web.on("exit", (code) => reject(new Error(`driga web exited ${code}`)));
      setTimeout(() => reject(new Error("driga web printed no URL")), 20000);
    });
    browser = await chromium.launch();
    context = await browser.newContext({ viewport: size, deviceScaleFactor: 1, colorScheme: scheme, recordVideo: { dir, size } });
    await context.addInitScript((z) => document.addEventListener("DOMContentLoaded", () => { document.body.style.zoom = String(z); }), zoom);
    await context.addInitScript(pointer);
    page = await context.newPage();
    at = { x: 640, y: 520 };
    await page.goto(url);
    await page.waitForSelector(".fb-node");
    // The prompt arrives at 8 s of demo time; Allow arms after a short delay.
    const allow = page.locator('[data-answer="allow"]').first();
    await allow.waitFor({ timeout: 30000 });
    await sleep(900);
    await glide(page, page.locator(".fb-need.primary code").first(), 900);
    await page.waitForFunction((el) => !el.disabled, await allow.elementHandle(), { timeout: 10000 });
    await sleep(1400);
    await glide(page, allow, 600);
    await sleep(350);
    await page.mouse.down();
    await sleep(90);
    await page.mouse.up();
    await sleep(2200);
    const node = page.locator('.fb-node[data-title="Add dark mode to settings"] circle[r="14"]');
    await glide(page, node, 1000);
    await sleep(300);
    await page.mouse.down();
    await sleep(90);
    await page.mouse.up();
    await sleep(3500);
    ok = true;
  } finally {
    const video = page?.video();
    await context?.close();
    await browser?.close();
    for (const child of children) child.kill();
    // A failed take is discarded, never published.
    if (video) {
      if (ok) await rename(await video.path(), join("out", `web-${name}-${scheme}.webm`));
      else await rm(await video.path(), { force: true });
    }
  }
}

await mkdir("out", { recursive: true });
for (const name of Object.keys(takes)) for (const scheme of ["dark", "light"]) await take(name, scheme);
