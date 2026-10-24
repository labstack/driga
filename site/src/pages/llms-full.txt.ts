import type { APIRoute } from "astro";
import { getCollection } from "astro:content";
import { toPlainMarkdown } from "../lib/markdown";

// Every page, concatenated, for an agent that would rather make one request
// than twenty. Ordered the way a reader meets the material.
const ROOT = "index";
const ORDER = [ROOT, "start", "guides", "reference", "explanation", "privacy"];

export const GET: APIRoute = async () => {
  const docs = await getCollection("docs");

  const unlisted = [...new Set(docs.map((doc) => doc.id.split("/")[0] ?? ROOT))].filter(
    (group) => !ORDER.includes(group),
  );
  if (unlisted.length > 0) {
    throw new Error(
      `llms-full.txt claims to be complete but would omit: ${unlisted.join(", ")}. ` +
        `Add them to ORDER in src/pages/llms-full.txt.ts.`,
    );
  }

  const ranked = [...docs].sort((a, b) => {
    const ga = ORDER.indexOf(a.id.split("/")[0] ?? ROOT);
    const gb = ORDER.indexOf(b.id.split("/")[0] ?? ROOT);
    return ga === gb ? a.id.localeCompare(b.id) : ga - gb;
  });

  const parts = [
    "# Driga — complete documentation",
    "",
    "Driga watches AI coding agent sessions across projects and paired machines:",
    "a CLI, a terminal UI, a loopback web dashboard and a read-only MCP server.",
    "Generated from the documentation source.",
    "",
  ];

  for (const doc of ranked) {
    parts.push("---", "");
    parts.push(`# ${doc.data.title}`, "");
    parts.push(`Path: /${doc.id}`);
    if (doc.data.summary) parts.push(`Summary: ${doc.data.summary}`);
    parts.push("", toPlainMarkdown(doc.body ?? ""), "");
  }

  return new Response(parts.join("\n"), {
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
};
