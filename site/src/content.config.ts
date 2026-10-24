import { defineCollection } from "astro:content";
import { z } from "astro/zod";
import { docsLoader } from "@astrojs/starlight/loaders";
import { docsSchema } from "@astrojs/starlight/schema";

// Two fields exist for agents rather than readers.
//
// `summary` is the one-line description that appears beside the page in
// llms.txt. Without it the index is a list of titles, which tells an agent what
// a page is called but not whether it is worth fetching.
//
// `read_when` states the situations in which this page is the right one:
// retrieval metadata written by the person who knows the answer.
//
// `generated` marks a page written by cmd/driga-docgen, which `just check`
// regenerates and compares. Edit the generator, never the page.
export const collections = {
  docs: defineCollection({
    loader: docsLoader(),
    schema: docsSchema({
      extend: z.object({
        summary: z.string().max(240).optional(),
        read_when: z.array(z.string()).optional(),
        generated: z.boolean().default(false),
      }),
    }),
  }),
};
