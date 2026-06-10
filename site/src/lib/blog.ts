import { getCollection, type CollectionEntry } from 'astro:content';
import getReadingTime from 'reading-time';

export type BlogPost = CollectionEntry<'blog'>;

/**
 * All publishable posts, newest first. Draft filtering is conventional in
 * Astro 5 (there is no built-in draft support), so every surface that lists
 * posts (index, post routes, RSS, prev/next) must go through this helper
 * rather than calling getCollection('blog') directly.
 *
 * Drafts stay visible in `astro dev` so they can be previewed locally.
 */
export async function getPublishedPosts(): Promise<BlogPost[]> {
  const posts = await getCollection('blog', ({ data }) =>
    import.meta.env.PROD ? data.draft !== true : true,
  );
  return posts.sort((a, b) => b.data.pubDate.valueOf() - a.data.pubDate.valueOf());
}

/** "4 min read", computed from the raw markdown body at build time. */
export function readingTimeOf(post: BlogPost): string {
  return getReadingTime(post.body ?? '').text;
}

/** Editorial date for article headers: "June 10, 2026". */
export function formatDate(date: Date): string {
  return date.toLocaleDateString('en-US', {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
    timeZone: 'UTC',
  });
}

/** Compact mono-label date for list rows, matching the changelog: "2026-06-10". */
export function isoDate(date: Date): string {
  return date.toISOString().slice(0, 10);
}

/** Distinct tags across published posts with usage counts, most-used first. */
export function tagCounts(posts: BlogPost[]): { tag: string; count: number }[] {
  const counts = new Map<string, number>();
  for (const post of posts) {
    for (const tag of post.data.tags) counts.set(tag, (counts.get(tag) ?? 0) + 1);
  }
  return [...counts.entries()]
    .map(([tag, count]) => ({ tag, count }))
    .sort((a, b) => b.count - a.count || a.tag.localeCompare(b.tag));
}
