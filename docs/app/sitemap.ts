import { source } from '@/lib/source';
import type { MetadataRoute } from 'next';

const BASE = 'https://docs.warmbly.com';

// Static export writes this to out/sitemap.xml at build time.
export const dynamic = 'force-static';

export default function sitemap(): MetadataRoute.Sitemap {
  return source.getPages().map((page) => ({
    url: `${BASE}${page.url.endsWith('/') ? page.url : `${page.url}/`}`,
    changeFrequency: 'weekly',
    priority: page.slugs.length <= 1 ? 0.8 : 0.5,
  }));
}
