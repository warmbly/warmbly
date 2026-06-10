import rss from '@astrojs/rss';
import { getCollection } from 'astro:content';

export async function GET(context) {
  // Filter drafts unconditionally: the feed should never carry them,
  // even when generated from a dev server.
  const posts = (await getCollection('blog', ({ data }) => data.draft !== true)).sort(
    (a, b) => b.data.pubDate.valueOf() - a.data.pubDate.valueOf(),
  );
  return rss({
    title: 'Warmbly Blog',
    description:
      'Engineering notes, policy decisions and sending guidance from the team building Warmbly.',
    site: context.site,
    items: posts.map((post) => ({
      title: post.data.title,
      description: post.data.description,
      pubDate: post.data.pubDate,
      link: `/blog/${post.id}/`,
    })),
    customData: '<language>en-us</language>',
  });
}
