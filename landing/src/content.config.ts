import { docsLoader } from "@astrojs/starlight/loaders";
import { docsSchema } from "@astrojs/starlight/schema";
import { glob } from "astro/loaders";
import { defineCollection } from "astro:content";
// `z` used to come from `astro:content`, which re-exported it and now marks that
// re-export deprecated -- five of `astro check`'s hints were this one import.
// Astro depends on `zod@^4.3.6`, so the direct dependency is pinned to the
// `4.4.3` already in the tree: a different major here would type the schemas
// against a different Zod than the one Astro validates them with.
import { z } from "zod";

const releases = defineCollection({
  loader: glob({ pattern: "**/*.md", base: "./src/content/releases" }),
  schema: z.object({
    version: z.string(),
    date: z.date(),
    requires_reindex: z.boolean().default(false),
  }),
});

const blog = defineCollection({
  loader: glob({ pattern: "**/*.md", base: "./src/content/blog" }),
  schema: z.object({
    title: z.string().min(1),
    description: z.string().min(1),
    pubDate: z.date(),
    updatedDate: z.date().optional(),
    author: z.string().default("Kivgraph"),
    category: z.string().min(1),
    tags: z.array(z.string()).default([]),
    faq: z
      .array(
        z.object({
          question: z.string().min(1),
          answer: z.string().min(1),
        }),
      )
      .default([]),
    draft: z.boolean().default(false),
    featured: z.boolean().default(false),
  }),
});

export const collections = {
  docs: defineCollection({ loader: docsLoader(), schema: docsSchema() }),
  releases,
  blog,
};
