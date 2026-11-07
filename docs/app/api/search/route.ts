import { source } from '@/lib/source';
import { createFromSource } from 'fumadocs-core/search/server';
import { findPath } from 'fumadocs-core/page-tree';
import type { StructuredData } from 'fumadocs-core/mdx-plugins';

// Build the search index at build time and serve it as a static JSON, so search
// works on a statically-exported / CDN-hosted build (no running server route).
// The client is set to `type: 'static'` in app/layout.tsx, which loads this
// index once and runs Orama in the browser.
export const revalidate = false;

// One entry per section instead of per paragraph: every entry costs index space, and Pages refuses files over 25 MiB.
function bySection(data: StructuredData): StructuredData {
  const contents: StructuredData['contents'] = [];
  for (const item of data.contents) {
    const last = contents.at(-1);
    if (last && last.heading === item.heading) last.content += `\n${item.content}`;
    else contents.push({ ...item });
  }
  return { ...data, contents };
}

export const { staticGET: GET } = createFromSource(source, {
  // https://docs.orama.com/docs/orama-js/supported-languages
  language: 'english',
  // Search ranks by relevance; the sort table only serves sortBy and kept the static index over Pages' 25 MiB file limit.
  sort: { enabled: false },
  async buildIndex(page) {
    const tree = source.getPageTree(page.locale);
    const path = findPath(tree.children, (node) => node.type === 'page' && node.url === page.url);
    const breadcrumbs = path?.slice(0, -1).flatMap((node) => (typeof node.name === 'string' && node.name ? [node.name] : []));
    const { structuredData } = page.data;
    return {
      id: page.url,
      url: page.url,
      title: page.data.title,
      description: page.data.description,
      breadcrumbs,
      structuredData: bySection(structuredData),
    };
  },
});
