import type { APIRoute } from "astro";
import { getCollection } from "astro:content";

// The index an agent reads first. Every entry carries the author's one-line
// summary, because a list of titles does not tell an agent whether a page is
// worth fetching.
//
// The landing page groups as "index", so it is listed explicitly; otherwise it
// is dropped from an index whose heading promises every page.
const ROOT = "index";
const ORDER = [ROOT, "start", "guides", "reference", "explanation", "privacy"];

const GROUP_TITLES: Record<string, string> = {
  [ROOT]: "Overview",
  start: "Start here",
  guides: "Guides",
  reference: "Reference",
  explanation: "Explanation",
  privacy: "Site terms",
};

export const GET: APIRoute = async ({ site }) => {
  const origin = (site ?? new URL("https://driga.dev")).origin;
  const docs = await getCollection("docs");

  const lines: string[] = [
    "# Driga",
    "",
    "Driga watches AI coding agent sessions (Claude Code, Codex, Gemini CLI,",
    "GitHub Copilot CLI) across projects and paired machines. It is a CLI, a",
    "terminal UI and a loopback web dashboard: Flow shows sessions as a map,",
    "timeline and list; Needs you collects waits and permission prompts; Projects",
    "and Machines show Git state, health and leftovers. Driga is alpha.",
    "",
    "> Use this file as a map of the Driga documentation. Fetch any page as",
    "> Markdown by appending `.md` to its URL.",
    "",
    "## Agent Resources",
    "",
    `- [Markdown page export](${origin}/start/install.md): Append \`.md\` to any docs page URL for clean Markdown.`,
    `- [Full documentation text](${origin}/llms-full.txt): Every page concatenated, for one-shot ingestion.`,
    `- [Sitemap](${origin}/sitemap-index.xml): Crawler URL index.`,
    "",
    "## Operating Driga from an agent",
    "",
    "- `driga mcp` serves read-only MCP tools over stdio; register it with `claude mcp add --scope user driga -- driga mcp`. Reading never acknowledges inbox items.",
    "- Every read command accepts `--json` and returns a schema-2 envelope with data, observed_at, source freshness and coverage gaps.",
    "- Commands that change state ask for confirmation; `--yes` applies without asking. Without a terminal and without `--yes` they exit 2 and change nothing.",
    "- Configuration is `config.toml` plus DRIGA_* environment variables; unknown DRIGA_* names are errors.",
    "",
  ];

  // Silently dropping a section is how a directory of documentation becomes
  // invisible to every agent while the site builds clean. Fail instead.
  const groups = new Set(docs.map((doc) => doc.id.split("/")[0] ?? ROOT));
  const unlisted = [...groups].filter((group) => !ORDER.includes(group));
  if (unlisted.length > 0) {
    throw new Error(
      `llms.txt would omit these documentation sections: ${unlisted.join(", ")}. ` +
        `Add them to ORDER and GROUP_TITLES in src/pages/llms.txt.ts.`,
    );
  }

  const unsummarised = docs.filter((doc) => !doc.data.summary && !doc.data.description);
  if (unsummarised.length > 0) {
    throw new Error(
      `these pages would be indexed with no summary: ${unsummarised
        .map((doc) => doc.id || "index")
        .join(", ")}`,
    );
  }

  const byGroup = new Map<string, typeof docs>();
  for (const doc of docs) {
    const group = doc.id.split("/")[0] ?? ROOT;
    const bucket = byGroup.get(group) ?? [];
    bucket.push(doc);
    byGroup.set(group, bucket);
  }

  for (const group of ORDER) {
    const entries = (byGroup.get(group) ?? []).sort((a, b) => a.id.localeCompare(b.id));
    if (entries.length === 0) continue;
    lines.push(`## ${GROUP_TITLES[group] ?? group}`, "");
    for (const entry of entries) {
      const summary = entry.data.summary ?? entry.data.description;
      lines.push(`- [${entry.data.title}](${origin}/${entry.id}.md): ${summary}`);
      for (const when of entry.data.read_when ?? []) {
        lines.push(`  - Read when: ${when}`);
      }
    }
    lines.push("");
  }

  return new Response(lines.join("\n"), {
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
};
