import { defineCollection, z } from 'astro:content';
import { glob } from 'astro/loaders';

// Blog posts: markdown/MDX files under src/content/blog.
// The file name (without extension) becomes the URL slug, e.g.
// src/content/blog/quarantine-early.mdx -> /blog/quarantine-early/
const blog = defineCollection({
  loader: glob({ pattern: '**/*.{md,mdx}', base: './src/content/blog' }),
  schema: z.object({
    title: z.string(),
    description: z.string(),
    pubDate: z.coerce.date(),
    updatedDate: z.coerce.date().optional(),
    author: z.string().default('Warmbly team'),
    // Short topic labels rendered as chips; not pages. Keep them few and reused.
    tags: z.array(z.string()).default([]),
    // Optional card/header image (path under /public, e.g. "/blog/foo.webp").
    // Posts without one get a generated sky cover.
    cover: z.string().optional(),
    draft: z.boolean().default(false),
  }),
});

export const collections = { blog };
