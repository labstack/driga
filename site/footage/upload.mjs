// Publishes out/media to the R2 bucket driga, served at
// https://assets.driga.dev, and records the URLs in src/data/media.json.
// Keys carry a content hash, so files are immutable and a retake never
// overwrites what a cached page still points at. Unchanged files are skipped.
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, readFileSync, readdirSync, renameSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const input = "out/media";
const manifestPath = "../src/data/media.json";
const manifest = existsSync(manifestPath) ? JSON.parse(readFileSync(manifestPath, "utf8")) : {};

for (const file of readdirSync(input).sort()) {
  const match = file.match(/^([a-z]+-(?:dark|light))\.(mp4|webm)$/);
  if (!match) continue;
  const [, name, format] = match;
  const contents = readFileSync(join(input, file));
  const key = `media/${name}-${createHash("sha256").update(contents).digest("hex").slice(0, 12)}.${format}`;
  const url = `https://assets.driga.dev/${key}`;
  if (manifest[name]?.[format] === url) continue;
  // Keys are content-addressed, so retrying a put is always safe.
  const args = ["r2", "object", "put", `driga/${key}`, "--file", join(input, file),
    "--content-type", `video/${format}`, "--cache-control", "public, max-age=31536000, immutable", "--remote"];
  let put;
  for (let attempt = 1; attempt <= 3; attempt++) {
    put = spawnSync("wrangler", args, { stdio: "inherit" });
    if (put.status === 0) break;
  }
  if (put.status !== 0) throw new Error(`upload of ${file} failed`);
  manifest[name] = { ...manifest[name], [format]: url };
}

// Written only after every upload succeeded, and never half-written.
const sorted = Object.fromEntries(Object.keys(manifest).sort().map((name) => [name, manifest[name]]));
writeFileSync(`${manifestPath}.tmp`, `${JSON.stringify(sorted, null, 2)}\n`);
renameSync(`${manifestPath}.tmp`, manifestPath);
